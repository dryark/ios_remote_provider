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
)

const (
    VID_NONE = iota
    VID_APP
    VID_BRIDGE
    VID_WDA
    VID_CFA
    VID_ENABLE
    VID_DISABLE
    VID_END
)

const (
    DEV_STOP = iota
    DEV_CFA_START
    DEV_CFA_START_ERR
    DEV_CFA_STOP
    DEV_WDA_START
    DEV_WDA_START_ERR
    DEV_WDA_STOP
    DEV_VIDEO_START
    DEV_VIDEO_STOP
    DEV_ALERT_APPEAR
    DEV_ALERT_GONE
    DEV_APP_CHANGED
)

type Device struct {
    udid            string
    name            string
    lock            *sync.Mutex
    wdaPort         int
    wdaPortFixed    bool
    cfaNngPort      int
    cfaNngPort2     int
    vidPort         int
    vidControlPort  int
    vidLogPort      int
    backupVideoPort int
    iosVersion      string
    versionParts    []int
    productType     string
    productNum      string
    vidWidth        int
    vidHeight       int
    vidMode         int
    process         map[string] *GenericProc
    owner           string
    connected       bool
    EventCh         chan DevEvent
    BackupCh        chan BackupEvent     
    CFAFrameCh      chan BackupEvent
    cfa             *CFA
    wda             *WDA
    cfaRunning      bool
    wdaRunning      bool
    devTracker      *DeviceTracker
    config          *Config
    devConfig       *CDevice
    cf              *ControlFloor
    info            map[string] string
    vidStreamer     VideoStreamer
    appStreamStopChan chan bool
    vidOut          *ws.Conn
    bridge          BridgeDev
    backupVideo     BackupVideo
    backupActive    bool
    shuttingDown    bool
    alertMode       bool
    vidUp           bool
}

func NewDevice( config *Config, devTracker *DeviceTracker, udid string, bdev BridgeDev ) (*Device) {
    dev := Device{
        devTracker:      devTracker,
        wdaPortFixed:    false,
        cfaNngPort:      devTracker.getPort(),
        cfaNngPort2:     devTracker.getPort(),
        vidPort:         devTracker.getPort(),
        vidLogPort:      devTracker.getPort(),
        vidMode:         VID_NONE,
        vidControlPort:  devTracker.getPort(),
        backupVideoPort: devTracker.getPort(),
        backupActive:    false,
        config:          config,
        udid:            udid,
        lock:            &sync.Mutex{},
        process:         make( map[string] *GenericProc ),
        cf:              devTracker.cf,
        EventCh:         make( chan DevEvent ),
        BackupCh:        make( chan BackupEvent ),
        CFAFrameCh:      make( chan BackupEvent ),
        bridge:          bdev,
        cfaRunning:      false,
        versionParts:    []int{0,0,0},
    }
    if devConfig, ok := config.devs[udid]; ok {
        dev.devConfig = &devConfig
        if devConfig.wdaPort != 0 {
            dev.wdaPort = devConfig.wdaPort
            dev.wdaPortFixed = true
        } else {
            dev.wdaPort = devTracker.getPort()
        }
    } else {
        dev.wdaPort = devTracker.getPort()
    }
    return &dev
}

func ( self *Device ) isShuttingDown() bool {
    return self.shuttingDown;
}

func ( self *Device ) releasePorts() {
    dt := self.devTracker
    if !self.wdaPortFixed {
        dt.freePort( self.wdaPort )
    }
    dt.freePort( self.cfaNngPort )
    dt.freePort( self.cfaNngPort2 )
    dt.freePort( self.vidPort )
    dt.freePort( self.vidLogPort )
    dt.freePort( self.vidControlPort )
    dt.freePort( self.backupVideoPort )
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
    data string
}

func (self *Device) shutdown() {
    self.shutdownVidStream()
  
    go func() { self.endProcs() }()
    
    go func() { self.EventCh <- DevEvent{ action: DEV_STOP } }()
    go func() { self.BackupCh <- BackupEvent{ action: VID_END } }()
    
    for _,proc := range self.process {
        log.WithFields( log.Fields{
            "type": "shutdown_dev_proc",
            "udid": censorUuid( self.udid ),
            "proc": proc.name,
            "pid":  proc.pid,
        } ).Info("Shutting down " + proc.name + " process")
        go func() { proc.Kill() }()
    }
}

func (self *Device) onCfaReady() {
    self.cfaRunning = true
    self.cf.notifyCfaStarted( self.udid )
    self.cfa.ensureSession()
    // start video streaming
    
    self.forwardVidPorts( self.udid, func() {
        videoMode := self.devConfig.videoMode 
        if videoMode == "app" {
            self.enableAppVideo()
        } else if videoMode == "cfagent" {
            self.enableCFAVideo()
        } else {
            // TODO error
        }
       
        self.startProcs2()
    } )
}

func (self *Device) onWdaReady() {
    self.wdaRunning = true
    self.cf.notifyWdaStarted( self.udid, self.wdaPort )
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
                } else if action == DEV_CFA_START { // CFA started
                    self.onCfaReady()
                } else if action == DEV_WDA_START { // CFA started
                    self.onWdaReady()
                } else if action == DEV_CFA_START_ERR {
                    fmt.Printf("Error starting/connecting to CFA.\n")
                    self.shutdown()
                    break DEVEVENTLOOP
                } else if action == DEV_CFA_STOP { // CFA stopped
                    self.cfaRunning = false
                    self.cf.notifyCfaStopped( self.udid )
                } else if action == DEV_CFA_STOP { // CFA stopped
                    self.wdaRunning = false
                    self.cf.notifyWdaStopped( self.udid )
                } else if action == DEV_VIDEO_START { // first video frame
                    self.cf.notifyVideoStarted( self.udid )
                    self.onFirstFrame( &event )
                } else if action == DEV_VIDEO_STOP {
                    self.cf.notifyVideoStopped( self.udid )
                } else if action == DEV_ALERT_APPEAR {
                    self.enableBackupVideo()
                } else if action == DEV_ALERT_GONE {
                    self.disableBackupVideo()
                } else if action == DEV_APP_CHANGED {
                    self.devAppChanged( event.data ) 
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
                    fmt.Printf("backup video frame sender - enabling\n")
                } else if action == VID_DISABLE {
                    sending = false
                    fmt.Printf("backup video frame sender - disabling\n")
                } else if action == VID_END {
                    break
                }
            default:
            }
            if sending {
                self.sendBackupFrame()
            } else {
                time.Sleep( time.Millisecond * 100 )
            }
        }
    }()
}

func (self *Device) startCFAFrameProvider() {
    go func() {
        sending := false
        for {
            select {
                case ev := <- self.CFAFrameCh:
                    action := ev.action
                    if action == VID_ENABLE {
                        sending = true
                        fmt.Printf("cfa frame provider - enabling\n")
                    } else if action == VID_DISABLE {
                        sending = false
                        fmt.Printf("cfa frame provider - disabled\n")
                    } else if action == VID_END {
                        break
                    }
                default:
            }
            if sending {
                self.sendCFAFrame()
            } else {
                time.Sleep( time.Millisecond * 100 )
            }
        }
    }()
}

func (self *Device) enableDefaultVideo() {
    videoMode := self.devConfig.videoMode 
    if videoMode == "app" {
        self.vidMode = VID_APP
        self.vidStreamer.forceOneFrame()
    } else if videoMode == "cfagent" {
        self.vidMode = VID_CFA
    } else {
        // TODO error
    }
}

func (self *Device) disableBackupVideo() {
    fmt.Printf("Sending vid_disable\n")
    self.BackupCh <- BackupEvent{ action: VID_DISABLE }
    fmt.Printf("Sent vid_disable\n")
    self.backupActive = false
    self.enableDefaultVideo()
}

func (self *Device) enableBackupVideo() {
    fmt.Printf("Sending vid_enable\n")
    self.BackupCh <- BackupEvent{ action: VID_ENABLE }
    fmt.Printf("Sent vid_enable\n")
    self.vidMode = VID_BRIDGE
    self.backupActive = true
}

func (self *Device) disableCFAVideo() {
    fmt.Printf("Sending vid_disable\n")
    self.CFAFrameCh <- BackupEvent{ action: VID_DISABLE }
    fmt.Printf("Sent vid_disable\n")
    
    self.enableDefaultVideo()
}

func (self *Device) enableCFAVideo() {
    fmt.Printf("Sending vid_enable\n")
    self.CFAFrameCh <- BackupEvent{ action: VID_ENABLE }
    fmt.Printf("Sent vid_enable\n")
    self.vidMode = VID_CFA
    self.backupActive = true
}

func (self *Device) sendBackupFrame() {
    vidOut := self.vidOut
    if vidOut != nil {
        fmt.Printf("Fetching frame - ")
        pngData := self.backupVideo.GetFrame()
        fmt.Printf("%d bytes\n", len( pngData ) )
        if( len( pngData ) > 0 ) {
            vidOut.WriteMessage( ws.BinaryMessage, pngData )
        }
    } else {
        time.Sleep( time.Millisecond * 100 )
    }
}

func (self *Device) sendCFAFrame() {
    vidOut := self.vidOut
    if vidOut != nil {
        pngData := self.cfa.Screenshot()
        //fmt.Printf("%d bytes\n", len( pngData ) )
        if( len( pngData ) > 0 ) {
            vidOut.WriteMessage( ws.BinaryMessage, pngData )
        }
    } else {
        time.Sleep( time.Millisecond * 100 )
    }
}

func (self *Device) getBackupFrame() ( []byte, string) {
    if self == nil {
        return []byte{}, "wtf"
    }
    if self.backupVideo == nil {
        return []byte{}, "backup video not set on device object"
    }
    
    pngData := self.backupVideo.GetFrame()
    
    return pngData, ""
}

func (self *Device) stopEventLoop() {
    self.EventCh <- DevEvent{ action: DEV_STOP }
}

func (self *Device) startup() {
    self.startEventLoop()
    self.startProcs()
}

func (self *Device) startBackupVideo() {
    self.backupVideo = self.bridge.NewBackupVideo( 
        self.backupVideoPort,
        func( interface{} ) {}, // onStop
    )
}

func (self *Device) devAppChanged( bundleId string ) {
    if self.cfa == nil {
        return
    }
    
    self.cfa.AppChanged( bundleId )
}

func (self *Device) startProcs() {
    // Start CFA
    self.cfa = NewCFA( self.config, self.devTracker, self )
        
    if self.config.cfaMethod == "manual" {
        //self.cfa.startCfaNng()
    }
    
    self.startBackupFrameProvider() // just the timed loop
    self.startCFAFrameProvider()
    self.backupVideo = self.bridge.NewBackupVideo( 
        self.backupVideoPort,
        func( interface{} ) {}, // onStop
    )
    
    //self.enableBackupVideo()
    
    self.bridge.NewSyslogMonitor( func( msg string, app string ) {
        //msg := root.GetAt( 3 ).String()
        //app := root.GetAt( 1 ).String()
        
        //fmt.Printf("Msg:%s\n", msg )
        
        if app == "SpringBoard(SpringBoard)" {
            if strings.Contains( msg, "Presenting <SBUserNotificationAlert" ) {
                alerts := self.config.alerts
                
                useAlertMode := true
                if len( alerts ) > 0 {
                    for _, alert := range alerts {
                        if strings.Contains( msg, alert.match ) {
                            fmt.Printf("Alert matching \"%s\" appeared. Autoresponding with \"%s\"\n",
                                alert.match, alert.response )
                            if self.cfaRunning {
                                useAlertMode = false
                                btn := self.cfa.GetEl( "button", alert.response, true, 0 )
                                if btn == "" {
                                    fmt.Printf("Alert does not contain button \"%s\"\n", alert.response )
                                } else {
                                    self.cfa.ElClick( btn )
                                }
                            }
                            
                        }
                    }
                }
                
                if useAlertMode && self.vidUp { 
                    fmt.Printf("Alert appeared\n")
                    if len( alerts ) > 0 {
                        fmt.Printf("Alert did not match any autoresponses; Msg content: %s\n", msg )
                    }
                    self.EventCh <- DevEvent{ action: DEV_ALERT_APPEAR }
                    self.alertMode = true
                }
            } else if strings.Contains( msg, "deactivate alertItem: <SBUserNotificationAlert" ) {
                if self.alertMode {
                    self.alertMode = false
                    fmt.Printf("Alert went away\n")
                    self.EventCh <- DevEvent{ action: DEV_ALERT_GONE }
                }
            }
        } else if app == "SpringBoard(FrontBoard)" {
            if strings.Contains( msg, "Setting process visibility to: Foreground" ) {
                fmt.Printf("Process vis line:%s\n", msg )
                appStr := "application<"
                index := strings.Index( msg, appStr )
                if index != -1 {
                    after := index + len( appStr )
                    left := msg[after:]
                    endPos := strings.Index( left, ">" )
                    app := left[:endPos]
                    fmt.Printf("app:%s\n", app )
                    self.EventCh <- DevEvent{ action: DEV_APP_CHANGED, data: app }
                }
            }
        } else if app == "dasd" {
            if strings.HasPrefix( msg, "Foreground apps changed" ) {
                //fmt.Printf("App changed\n")
                //self.EventCh <- DevEvent{ action: DEV_APP_CHANGED }
            }
        }
    } )
}

func (self *Device) startProcs2() {
    self.appStreamStopChan = make( chan bool )
    
    videoMode := self.devConfig.videoMode 
    if videoMode == "app" {
        self.vidStreamer = NewAppStream(
            self.appStreamStopChan,
            self.vidControlPort,
            self.vidPort,
            self.vidLogPort,
            self.udid,
            self )
        self.vidStreamer.mainLoop()
    } else if videoMode == "cfagent" {
        // Nothing todo
    } else {
        // TODO error
    }
    
    // Start WDA
    self.wda = NewWDA( self.config, self.devTracker, self )
}

func (self *Device) vidAppIsAlive() bool {
    vidPid := self.bridge.GetPid( self.config.vidAppExtBid )
    if vidPid != 0 {
        return true
    }
    return false
}

func (self *Device) enableAppVideo() {
    // check if video app is running
    vidPid := self.bridge.GetPid( self.config.vidAppExtBid )
    
    // if it is running, go ahead and use it
    /*if vidPid != 0 {
        self.vidMode = VID_APP
        return
    }*/
    
    // If it is running, kill it
    if vidPid != 0 {
        self.bridge.Kill( vidPid )
        
        // Kill off replayd in case it is stuck
        rp_id := self.bridge.GetPid("replayd")
        if rp_id != 0 {
            self.bridge.Kill( rp_id )
        }
    }
    
    // if video app is not running, check if it is installed
    
    bid := self.config.vidAppBidPrefix + "." + self.config.vidAppBid
    
    installInfo := self.bridge.AppInfo( bid )
    // if installed, start it                                             
    if installInfo != nil {
        fmt.Printf("Attempting to start video app stream\n")
        version := installInfo.Get("CFBundleShortVersionString").String()
        
        if version != "1.1" {
            fmt.Printf("Installed CF Vidstream app is version %s; must be version 1.1\n", version)
            panic("Wrong vidstream version")
        }
      
        self.cfa.StartBroadcastStream( self.config.vidAppName, bid, self.devConfig )
        self.vidUp = true
        self.vidMode = VID_APP
        return
    }
    
    fmt.Printf("Vidstream not installed; attempting to install\n")
    
    // if video app is not installed
    // install it, then start it
    success := self.bridge.InstallApp( "vidstream.xcarchive/Products/Applications/vidstream.app" )
    if success {
        self.cfa.StartBroadcastStream( self.config.vidAppName, bid, self.devConfig )
        self.vidMode = VID_APP
        return
    }
    
    // if video app failed to start or install, just leave backup video running
}

func (self *Device) justStartBroadcast() {
    bid := self.config.vidAppBidPrefix + "." + self.config.vidAppBid
    self.cfa.StartBroadcastStream( self.config.vidAppName, bid, self.devConfig )
}

func (self *Device) startVidStream() {
    conn := self.cf.connectVidChannel( self.udid )
    
    imgData := self.cfa.Screenshot()
    conn.WriteMessage( ws.BinaryMessage, imgData )
    
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
        //conn.WriteMessage( ws.TextMessage, []byte( fmt.Sprintf("{\"action\":\"normalFrame\"}") ) )
        conn.WriteMessage( ws.TextMessage, []byte( text ) )
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

func (self *Device) shutdownVidStream() {
    if self.vidOut != nil {
        self.stopVidStream()
    }
    ext_id := self.bridge.GetPid("vidstream_ext")
    if ext_id != 0 {
        self.bridge.Kill( ext_id )
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
        TunPair{ from: self.vidLogPort, to: 8353 },
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
    self.cfa.clickAt( x, y )
}

func (self *Device) mouseDown( x int, y int ) {
    self.cfa.mouseDown( x, y )
}

func (self *Device) mouseUp( x int, y int ) {
    self.cfa.mouseUp( x, y )
}

func (self *Device) hardPress( x int, y int ) {
    self.cfa.hardPress( x, y )
}

func (self *Device) longPress( x int, y int ) {
    self.cfa.longPress( x, y )
}

func (self *Device) home() {
    self.cfa.home()
}

func findNodeWithAtt( cur uj.JNode, att string, label string ) uj.JNode {
    lNode := cur.Get(att)
    if lNode != nil {
        if lNode.String() == label { return cur }
    }

    cNode := cur.Get("c")
    if cNode == nil { return nil }
    
    var gotIt uj.JNode
    cNode.ForEach( func( child uj.JNode ) {
        res := findNodeWithAtt( child, att, label )
        if res != nil { gotIt = res }
    } )
    return gotIt
}

// Assumes AssistiveTouch is enabled already
func (self *Device) openAssistiveTouch( pid int32 ) int {
    y := 0
    i := 0
    for {
        i++
        if i>10 {
            fmt.Printf("AssistiveTouch icon did not appear\n")
            return 0
        }
        json := self.cfa.ElByPid( int(pid), true )
        // Todo; element may not be there
        root, _ := uj.Parse( []byte(json) )
        btnNode := findNodeWithAtt( root, "label", "AssistiveTouch menu" )
        if btnNode == nil {
            time.Sleep( time.Millisecond * 100 )
            continue
        }
        x := btnNode.Get("x").Int()
        y = btnNode.Get("y").Int()
        x += 20
        y += 20
        time.Sleep( time.Millisecond * 100 )
        self.cfa.clickAt( x / 2, y / 2 )
        break
    }
    
    return y
}

func (self *Device) taskSwitcher() {
    //self.cfa.Siri("activate assistivetouch")
    
    self.enableAssistiveTouch()
    
    _, pid := self.isAssistiveTouchEnabled()
    
    y := self.openAssistiveTouch( pid )
    
    i := 0
    for {
        i++
        if i>10 {
            fmt.Printf("Could not find multitasking button")
            return
        }
        
        // TODO don't use hardcoded screen center
        atJson := self.cfa.AppAtPoint( 187, y/2, true, true, false )
        fmt.Println( atJson )
        
        root2, _ := uj.Parse( []byte(atJson) )
        taskNode := findNodeWithAtt( root2, "label", "Multitasking" )
        if taskNode == nil {
            time.Sleep( time.Millisecond * 100 )
            continue
        }
        x2 := taskNode.Get("x").Int()
        y2 := taskNode.Get("y").Int()
        x2 += 20
        y2 += 20
        time.Sleep( time.Millisecond * 200 )
        self.cfa.clickAt( x2 / 2, y2 / 2 )
        break
    }
    
    // Todo: Wait for task switcher to actually appear
    //time.Sleep( time.Millisecond * 600 )
    //self.cfa.GetEl("other", "SBSwitcherWindow", false, 1 )
    i = 0
    for {
        i++
        if i>20 {
            fmt.Printf("Task Switcher did not appear\n")
            return
        }
        centerScreenJson := self.cfa.AppAtPoint( 187, 333, true, true, true );
        root3, _ := uj.Parse( []byte(centerScreenJson) )
        closeBox := findNodeWithAtt( root3, "id", "appCloseBox" )
        if closeBox != nil { break }
        time.Sleep( time.Millisecond * 100 )
        //fmt.Printf("Task switcher appeared\n")
    }
    
    self.disableAssistiveTouch()
}

func (self *Device) shake() {
    self.enableAssistiveTouch()
    
    self.disableAssistiveTouch()
}

func (self *Device) cc() {
    self.cfa.OpenControlCenter()
}

func (self *Device) isAssistiveTouchEnabled() (bool, int32) {
    var pid int32
    procs := self.bridge.ps()
    for _,proc := range procs {
        if proc.name == "assistivetouchd" {
            pid = proc.pid
            break
        } 
    }
    if pid != 0 { return true, pid }
    return false, 0
}

func (self *Device) enableAssistiveTouch() {
    enabled, _ := self.isAssistiveTouchEnabled()
    if !enabled { self.toggleAssistiveTouch() }
    
    /*i := 0
    var pid int32
    for {
        i++
        if i> 20 { // Wait up to 4 seconds for it to start
            fmt.Printf("AssistiveTouch process did not start")
            return
        }
        
        procs := self.bridge.ps()
        for _,proc := range procs {
            if proc.name == "assistivetouchd" {
                pid = proc.pid
                break
            } 
        }
        if pid != 0 { break }
        time.Sleep( time.Millisecond * 200 )
    }*/
}

func (self *Device) disableAssistiveTouch() {
    enabled, _ := self.isAssistiveTouchEnabled()
    if enabled { self.toggleAssistiveTouch() }
    
    /*i = 0
    for {
        i++
        if i > 20 { // Wait up to 4 seconds for it to stop
            fmt.Printf("AssistiveTouch process did not stop")
            return
        }
        
        procs := self.bridge.ps()
        pid = 0
        for _,proc := range procs {
            if proc.name == "assistivetouchd" {
                pid = proc.pid
                break
            } 
        }
        if pid == 0 { break }
        time.Sleep( time.Millisecond * 200 )
    }*/
}

func (self *Device) toggleAssistiveTouch() {
    cfa := self.cfa
    self.cc()
    shortcutsBtn := cfa.GetEl( "button", "Accessibility Shortcuts", true, 2 )
    cfa.ElClick( shortcutsBtn )
    atBtn := cfa.GetEl( "button", "AssistiveTouch", true, 2 )
    cfa.ElClick( atBtn )
    time.Sleep( time.Millisecond * 100 )
    cfa.home()
    time.Sleep( time.Millisecond * 300 )
    cfa.home()
}

func (self *Device) iohid( page int, code int ) {
    self.cfa.ioHid( page, code )
}

func (self *Device) swipe( x1 int, y1 int, x2 int, y2 int, delayBy100 int ) {
    delay := float64( delayBy100 ) / 100.0
    self.cfa.swipe( x1, y1, x2, y2, delay )
}

func (self *Device) keys( keys string ) {
    parts := strings.Split( keys, "," )
    codes := []int{}
    for _, key := range parts {
        code, _ := strconv.Atoi( key )
        codes = append( codes, code )
    }
    self.cfa.keys( codes )
}

func (self *Device) source() string {
    return self.cfa.SourceJson()
}

func (self *Device) WifiIp() string {
    return self.cfa.WifiIp()
}

func (self *Device) AppAtPoint(x int, y int) string {
    return self.cfa.AppAtPoint(x,y,false,false,false)
}

func (self *Device) WifiMac() string {
    info := self.bridge.info( []string{"WiFiAddress"} )
    val, ok := info["WiFiAddress"]
    if ok { return val }
    return "unknown"
}

func (self *Device) killBid( bid string ) {
    self.bridge.KillBid( bid )
}

func (self *Device) launch( bid string ) {
    self.bridge.Launch( bid )
}