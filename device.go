package main

import (
    "fmt"
    "os"
    "sync"
    "time"
    log "github.com/sirupsen/logrus"
    ws "github.com/gorilla/websocket"
    //gocmd "github.com/go-cmd/cmd"
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
    wda         *WDA
    devTracker  *DeviceTracker
    config      *Config
    cf          *ControlFloor
    info        map[string] string
    vidStreamer VideoStreamer
    appStreamStopChan chan bool
}

type DevEvent struct {
    action int
    width      int
    height     int
}

func (self *Device) shutdown() {
    go func() { self.EventCh <- DevEvent{ action: 0 } }()
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

type ImgForwarder struct {
}

func (*ImgForwarder) consume( int ) {
    
}

func (self *Device) startStream( conn *ws.Conn ) {
    controlChan := self.vidStreamer.getControlChan()
    
    imgConsumer := NewImageConsumer( func( text string, data []byte ) {
        fmt.Printf("Image consume\n")
        conn.WriteMessage( ws.BinaryMessage, data )
    } )
    
    self.vidStreamer.setImageConsumer( imgConsumer )
    
    fmt.Printf("Telling video stream to start\n")
    controlChan <- 1 // start
}

func (self *Device) forwardVidPorts( udid string ) {
    curDir, _ := os.Getwd()
    
    o := ProcOptions {
        procName: "vidPortForward-"+udid,
        binary: curDir + "/" + self.config.mobiledevicePath,
        startFields: log.Fields{
            "vidPort": self.vidPort,
            "controlPort": self.vidControlPort,
            "udid": censorUuid( udid ),
        },
        args: []string{
            "tunnel",
            "-u", udid,
            fmt.Sprintf("%d:%d,%d:%d", self.vidPort, 8352, self.vidControlPort, 8351 ),
        },
    }
    
    proc_generic( self.devTracker, self, &o )
    /*args := []string {
        "tunnel",
        "-u", udid,
        fmt.Sprintf("%d:%d,%d:%d", self.vidPort, 8352, self.vidControlPort, 8351 ),
    }
    cmd := gocmd.NewCmdOptions( gocmd.Options{ Streaming: true }, self.config.mobiledevicePath, args... )
    cmd.Start()*/
    time.Sleep( time.Second * 3 )
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