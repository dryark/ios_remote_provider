package main

import (
    "fmt"
    "strings"
    "strconv"
    "sync"
    "time"
    log "github.com/sirupsen/logrus"
    ws "github.com/gorilla/websocket"
    uj "github.com/nanoscopic/ujsonin/v2/mod"
    //"go.nanomsg.org/mangos/v3"
    //nanoReq  "go.nanomsg.org/mangos/v3/protocol/req"
)

const (
    VID_NONE = iota
    VID_APP
    VID_BRIDGE
    VID_WDA
    VID_ENABLE
    VID_DISABLE
    VID_END
)

const (
    DEV_STOP = iota
    DEV_WDA_START
    DEV_WDA_STOP
    DEV_VIDEO_START
    DEV_VIDEO_STOP
    DEV_ALERT_APPEAR
    DEV_ALERT_GONE
)

type Device struct {
    udid        string
    name        string
    lock        *sync.Mutex
    wdaPort     int
    vidPort     int
    vidControlPort  int
    backupVideoPort int
    iosVersion  string
    productType string
    productNum  string
    vidWidth    int
    vidHeight   int
    vidMode     int
    process     map[string] *GenericProc
    owner       string
    connected   bool
    EventCh     chan DevEvent
    BackupCh    chan BackupEvent
    wda         *WDA
    wdaRunning  bool
    devTracker  *DeviceTracker
    config      *Config
    devConfig   *CDevice
    cf          *ControlFloor
    info        map[string] string
    vidStreamer VideoStreamer
    appStreamStopChan chan bool
    vidOut      *ws.Conn
    bridge      BridgeDev
    backupVideo *BackupVideo
    backupActive bool
}

func NewDevice( config *Config, devTracker *DeviceTracker, udid string, bdev BridgeDev ) (*Device) {
    dev := Device{
        devTracker: devTracker,
        wdaPort:    devTracker.getPort(),
        vidPort:    devTracker.getPort(),
        vidMode:    VID_NONE,
        vidControlPort:  devTracker.getPort(),
        backupVideoPort: devTracker.getPort(),
        backupActive: false,
        config:     config,
        udid:       udid,
        lock:       &sync.Mutex{},
        process:    make( map[string] *GenericProc ),
        cf:         devTracker.cf,
        EventCh:    make( chan DevEvent ),
        BackupCh:   make( chan BackupEvent ),
        bridge:     bdev,
        wdaRunning: false,
    }
    if devConfig, ok := config.devs[udid]; ok {
        dev.devConfig = &devConfig
    }
    return &dev
}

func ( self *Device ) startProc( proc *GenericProc ) {
    self.lock.Lock()
    self.process[ proc.name ] = proc
    self.lock.Unlock()
}

func ( self *Device ) stopProc( procName string ) {
    self.lock.Lock()
    delete( self.process, procName )
    self.lock.Unlock()
}

type BackupEvent struct {
    action int
}

type DevEvent struct {
    action int
    width  int
    height int
}

func (self *Device) shutdown() {
    go func() { self.EventCh <- DevEvent{ action: DEV_STOP } }()
    go func() { self.BackupCh <- BackupEvent{ action: VID_END } }()
    
    for _,proc := range self.process {
        log.WithFields( log.Fields{
            "type": "shutdown_dev_proc",
            "udid": censorUuid( self.udid ),
            "proc": proc.name,
            "pid":  proc.pid,
        } ).Info("Shutdown proc")
        go func() { proc.Kill() }()
    }
}

func (self *Device) startEventLoop() {
    go func() {
        DEVEVENTLOOP:
        for {
            select {
            case event := <- self.EventCh:
                action := event.action
                if action == DEV_STOP { // stop event loop
                    break DEVEVENTLOOP
                } else if action == DEV_WDA_START { // WDA started
                    self.wdaRunning = true
                    self.cf.notifyWdaStarted( self.udid )
                    self.wda.ensureSession()
                    // start video streaming
                } else if action == DEV_WDA_STOP { // WDA stopped
                    self.wdaRunning = false
                    self.cf.notifyWdaStopped( self.udid )
                } else if action == DEV_VIDEO_START { // first video frame
                    self.onFirstFrame( &event )
                } else if action == DEV_VIDEO_STOP {
                } else if action == DEV_ALERT_APPEAR {
                    self.enableBackupVideo()
                } else if action == DEV_ALERT_GONE {
                    self.disableBackupVideo()
                }
            }
        }
    }()
}

func (self *Device) startBackupFrameProvider() {
    go func() {
        sending := false
        for {
            select {
            case ev := <- self.BackupCh:
                action := ev.action
                if action == VID_ENABLE { // begin sending backup frames
                    sending = true
                } else if action == VID_DISABLE {
                    sending = false
                } else if action == VID_END {
                    break
                }
            default:
            }
            if sending {
                self.sendBackupFrame()
                time.Sleep( time.Millisecond * 500 )
            } else {
                time.Sleep( time.Millisecond * 100 )
            }
        }
    }()
}

func (self *Device) disableBackupVideo() {
    self.BackupCh <- BackupEvent{ action: VID_DISABLE }
    self.vidMode = VID_APP
    self.backupActive = false
}

func (self *Device) enableBackupVideo() {
    self.BackupCh <- BackupEvent{ action: VID_ENABLE }
    self.vidMode = VID_BRIDGE
    self.backupActive = true
}

func (self *Device) sendBackupFrame() {
    if self.vidOut != nil {
        //fmt.Printf("Fetching frame - ")
        pngData := self.backupVideo.GetFrame()
        //fmt.Printf("%d bytes\n", len( pngData ) )
        if( len( pngData ) > 0 ) {
            self.vidOut.WriteMessage( ws.BinaryMessage, pngData )
        }
    }
}

func (self *Device) stopEventLoop() {
    self.EventCh <- DevEvent{ action: DEV_STOP }
}

func (self *Device) startup() {
    self.startEventLoop()
    self.startProcs()
}

func (self *Device) startProcs() {
    // start wda
    self.wda = NewWDA( self.config, self.devTracker, self, self.wdaPort )
    
    self.startBackupFrameProvider() // just the timed loop
    self.backupVideo = self.bridge.NewBackupVideo( 
        self.backupVideoPort,
        func( interface{} ) {}, // onStop
    )
    
    self.enableVideo()
    //self.enableBackupVideo()
    
    self.bridge.NewSyslogMonitor( func( root uj.JNode ) {
        msg := root.GetAt( 3 ).String()
        
        //fmt.Printf("Msg:%s\n", msg )
        
        if strings.Contains( msg, "Presenting <SBUserNotificationAlert" ) {
            fmt.Printf("Alert appeared\n")
            self.EventCh <- DevEvent{ action: DEV_ALERT_APPEAR }
        } else if strings.Contains( msg, "deactivate alertItem: <SBUserNotificationAlert" ) {
            fmt.Printf("Alert went away\n")
            self.EventCh <- DevEvent{ action: DEV_ALERT_APPEAR }
        }
    } )
    
    self.forwardVidPorts( self.udid, func() {
        self.startProcs2()
    } )
}

func (self *Device) startProcs2() {
    self.appStreamStopChan = make( chan bool )
    self.vidStreamer = NewAppStream( self.appStreamStopChan, self.vidControlPort, self.vidPort, self.udid )
    self.vidStreamer.mainLoop()
}

func (self *Device) enableVideo() {
    // check if video app is running
    vidPid := self.bridge.GetPid( "vidtest2" )
    // if it is running, go ahead and use it
    if vidPid != 0 {
        self.vidMode = VID_APP
        return
    }
    
    self.wda.ensureSession()
    
    controlCenterMethod := "bottomUp"
    if self.devConfig != nil {
        controlCenterMethod = self.devConfig.controlCenterMethod
    }
    
    // if video app is not running, check if it is installed
    installInfo := self.bridge.AppInfo( "vidtest2" )
    // if installed, start it
    if installInfo != nil {
        self.wda.StartBroadcastStream( "vidtest2", controlCenterMethod )
        self.vidMode = VID_APP
        return
    }
    
    // if video app is not installed
    // install it, then start it
    success := self.bridge.InstallApp( "vidtest.xcarchive/Products/Applications/vidtest.app" )
    if success {
        self.wda.StartBroadcastStream( "vidtest2", controlCenterMethod )
        self.vidMode = VID_APP
        return
    }
    
    // if video app failed to start or install, just leave backup video running
}

func (self *Device) startVidStream() { // conn *ws.Conn ) {
    conn := self.cf.connectVidChannel( self.udid )
    
    var controlChan chan int
    if self.vidStreamer != nil {
        controlChan = self.vidStreamer.getControlChan()
    }
    
    // Necessary so that writes to the socket fail when the connection is lost
    go func() {
        for {
            if _, _, err := conn.NextReader(); err != nil {
                conn.Close()
                break
            }
        }
    }()
    
    self.vidOut = conn
    
    imgConsumer := NewImageConsumer( func( text string, data []byte ) (error) {
        if self.vidMode != VID_APP { return nil }
        // conn.WriteMessage( ws.TextMessage, []byte( fmt.Sprintf("{\"action\":\"normalFrame\"}") ) )
        return conn.WriteMessage( ws.BinaryMessage, data )
    }, func() {
        // there are no frames to send
    } )
    
    if self.vidStreamer != nil {
        self.vidStreamer.setImageConsumer( imgConsumer )
        fmt.Printf("Telling video stream to start\n")
        controlChan <- 1 // start
    }
}

func (self *Device) stopVidStream() {
    self.vidOut = nil
    self.cf.destroyVidChannel( self.udid )
}

func (self *Device) forwardVidPorts( udid string, onready func() ) {
    self.bridge.tunnel( []TunPair{
        TunPair{ from: self.vidPort, to: 8352 },
        TunPair{ from: self.vidControlPort, to: 8351 },
    }, onready )
} 

func (self *Device) endProcs() {
    if self.appStreamStopChan != nil {
        self.appStreamStopChan <- true
    }
}

func (self *Device) onFirstFrame( event *DevEvent ) {
    self.vidWidth = event.width
    self.vidWidth = event.height
    log.WithFields( log.Fields{
        "type":       "first_frame",
        "proc":       "ios_video_stream",
        "width":      self.vidWidth,
        "height":     self.vidWidth,
        "udid":       censorUuid( self.udid ),
    } ).Info("Video - first frame")
}

func (self *Device) clickAt( x int, y int ) {
    self.wda.clickAt( x, y )
}

func (self *Device) hardPress( x int, y int ) {
    self.wda.hardPress( x, y )
}

func (self *Device) longPress( x int, y int ) {
    self.wda.longPress( x, y )
}

func (self *Device) home() {
    self.wda.home()
}

func (self *Device) swipe( x1 int, y1 int, x2 int, y2 int ) {
    self.wda.swipe( x1, y1, x2, y2 )
}

func (self *Device) keys( keys string ) {
    parts := strings.Split( keys, "," )
    codes := []int{}
    for _, key := range parts {
        code, _ := strconv.Atoi( key )
        codes = append( codes, code )
    }
    self.wda.keys( codes )
}