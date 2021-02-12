package main

import (
    "fmt"
    //"os"
    "sync"
    "time"
    log "github.com/sirupsen/logrus"
    ws "github.com/gorilla/websocket"
    //gocmd "github.com/go-cmd/cmd"
    "go.nanomsg.org/mangos/v3"
	//nanoPull "go.nanomsg.org/mangos/v3/protocol/pull"
	nanoReq  "go.nanomsg.org/mangos/v3/protocol/req"
)

type Device struct {
    uuid        string
    name        string
    lock        *sync.Mutex
    wdaPort     int
    vidPort     int
    vidControlPort    int
    iosVersion  string
    productType string
    productNum  string
    vidWidth    int
    vidHeight   int
    process     map[string] *GenericProc
    owner       string
    connected   bool
    EventCh     chan DevEvent
    BackupCh    chan BackupEvent
    backupSock  mangos.Socket
    wda         *WDA
    devTracker  *DeviceTracker
    config      *Config
    cf          *ControlFloor
    info        map[string] string
    vidStreamer VideoStreamer
    appStreamStopChan chan bool
    vidOut      *ws.Conn
    bridge      BridgeDev
}

func NewDevice( config *Config, devTracker *DeviceTracker, uuid string, bdev BridgeDev ) (*Device) {
    dev := Device{
        devTracker: devTracker,
        wdaPort:    devTracker.getPort(),
        vidPort:   devTracker.getPort(),
        vidControlPort:   devTracker.getPort(),
        config:     config,
        uuid:       uuid,
        lock:       &sync.Mutex{},
        process:    make( map[string] *GenericProc ),
        cf:         devTracker.cf,
        EventCh:    make( chan DevEvent ),
        BackupCh:   make( chan BackupEvent ),
        bridge:     bdev,
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
    width      int
    height     int
}

func (self *Device) shutdown() {
    go func() { self.EventCh <- DevEvent{ action: 0 } }()
    go func() { self.BackupCh <- BackupEvent{ action: 2 } }()
    
    for _,proc := range self.process {
        log.WithFields( log.Fields{
            "type": "shutdown_dev_proc",
            "uuid": censorUuid( self.uuid ),
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
                if action == 0 { // stop event loop
                    break DEVEVENTLOOP
                } else if action == 1 { // WDA started
                    self.cf.notifyWdaStarted( self.uuid )
                    self.wda.ensureSession()
                    // start video streaming
                } else if action == 2 { // WDA stopped
                    self.cf.notifyWdaStopped( self.uuid )
                } else if action == 3 { // first video frame
                    self.onFirstFrame( &event )
                }
            }
        }
    }()
}

func (self *Device) startBackupFrameProvider() {
    go func() {
        sending := false
        for {
            time.Sleep( time.Millisecond * 200 ) // 5 times a second
            select {
            case ev := <- self.BackupCh:
                action := ev.action
                if action == 0 { // begin sending backup frames
                    sending = true
                } else if action == 1 {
                    sending = false
                } else if action == 2 {
                    break
                }        
            }
            if sending {
                self.sendBackupFrame()
            }
        }
    }()
}

func (self *Device) openBackupStream() {
    var err error
    
    spec := "tcp://127.0.0.1:8912"
    
    if self.backupSock, err = nanoReq.NewSocket(); err != nil {
        log.WithFields( log.Fields{
            "type":     "err_socket_new",
            "zmq_spec": spec,
            "err":      err,
        } ).Info("Socket new error")
        return
    }
    
    if err = self.backupSock.Dial( spec ); err != nil {
        log.WithFields( log.Fields{
            "type": "err_socket_dial",
            "spec": spec,
            "err":  err,
        } ).Info("Socket dial error")
        return
    }
}

func (self *Device) sendBackupFrame() {
    self.backupSock.Send([]byte("img"))
    
    msg, _ := self.backupSock.RecvMsg()
    self.vidOut.WriteMessage( ws.BinaryMessage, msg.Body )
}

func (self *Device) stopEventLoop() {
    self.EventCh <- DevEvent{
        action: 0,
    }
}

func (self *Device) startProcs() {
    // start wda
    self.wda = NewWDA( self.config, self.devTracker, self, self.wdaPort )
    //proc_ios_video_stream( self.devTracker, self )
    
    self.forwardVidPorts( self.uuid )
    self.appStreamStopChan = make( chan bool )
    self.vidStreamer = NewAppStream( self.appStreamStopChan, self.vidControlPort, self.vidPort, self.uuid )
    self.vidStreamer.mainLoop()
}

func (self *Device) startStream( conn *ws.Conn ) {
    controlChan := self.vidStreamer.getControlChan()
    
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
    
    backupActive := false
    
    imgConsumer := NewImageConsumer( func( text string, data []byte ) (error) {
        //fmt.Printf("Image consume\n")
        if backupActive {
            self.BackupCh <- BackupEvent{
                action: 1,
            }
            backupActive = false
            conn.WriteMessage( ws.TextMessage, []byte( fmt.Sprintf("{\"action\":\"normalFrame\"}") ) )
        }
        return conn.WriteMessage( ws.BinaryMessage, data )
    }, func() {
        // there are no frames to send
        backupActive = true
        conn.WriteMessage( ws.TextMessage, []byte( fmt.Sprintf("{\"action\":\"backupFrame\"}") ) )
    
        self.BackupCh <- BackupEvent{
            action: 0,
        }
    } )
    
    self.vidStreamer.setImageConsumer( imgConsumer )
    
    fmt.Printf("Telling video stream to start\n")
    controlChan <- 1 // start
}

func (self *Device) forwardVidPorts( udid string ) {
    self.bridge.tunnel( []TunPair{
        TunPair{ from: self.vidPort, to: 8352 },
        TunPair{ from: self.vidControlPort, to: 8351 },
    } )
    //curDir, _ := os.Getwd()
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
        "uuid":       censorUuid( self.uuid ),
    } ).Info("Video - first frame")
}

func (self *Device) clickAt( x int, y int ) {
    self.wda.clickAt( x, y )
}

func (self *Device) home() {
    self.wda.home()
}

func (self *Device) swipe( x1 int, y1 int, x2 int, y2 int ) {
    self.wda.swipe( x1, y1, x2, y2 )
}