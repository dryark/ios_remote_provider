package main

import (
    "fmt"
    "os"
    "time"
    
    "go.nanomsg.org/mangos/v3"
    nanoPull "go.nanomsg.org/mangos/v3/protocol/pull"
    nanoReq  "go.nanomsg.org/mangos/v3/protocol/req"
    
    // register transports
    _ "go.nanomsg.org/mangos/v3/transport/all"
    
    log "github.com/sirupsen/logrus"
)

type ImageConsumer struct {
    consumer func( string, []byte ) (error)
    noframesf func()
    udid string
}

func NewImageConsumer( consumer func( string, []byte ) (error), noframes func() ) (*ImageConsumer) {
    self := &ImageConsumer{
        consumer: consumer,
        noframesf: noframes,
    }
    return self
}

func (self *ImageConsumer) consume( text string, bytes []byte ) (error) {
    return self.consumer( text, bytes )
}

func (self *ImageConsumer) noframes() {
    self.noframesf()
}

type VideoStreamer interface {
    mainLoop()
    getControlChan() ( chan int )
    setImageConsumer( imgConsumer *ImageConsumer )
}

type AppStream struct {
    stopChan chan bool
    imgHandler *ImgHandler
    controlSpec string
    vidSpec string
    udid string
    controlSocket mangos.Socket
}

func NewAppStream( stopChan chan bool, controlPort int, vidPort int, udid string ) (*AppStream) {
    self := &AppStream{
        stopChan: stopChan,
        imgHandler: NewImgHandler( stopChan, udid ),
        controlSpec: fmt.Sprintf( "tcp://127.0.0.1:%d", controlPort ),
        vidSpec: fmt.Sprintf( "tcp://127.0.0.1:%d", vidPort ),
        udid: udid,
    }
    return self
}

func (self *AppStream) setImageConsumer( imgConsumer *ImageConsumer ) {
    self.imgHandler.setImageConsumer( imgConsumer )
    if self.controlSocket != nil {
        self.controlSocket.Send([]byte(`{"action": "oneframe"}`))
    }
}

func (self *AppStream) getControlChan() ( chan int ) {
    return self.imgHandler.mainCh
}

func (self *AppStream) openControl() (mangos.Socket,bool,chan bool) {
    var controlSocket mangos.Socket
    var controlStopChan chan bool
    failures := 0
    for {
        select {
            case <- self.stopChan: return nil,true,nil
            default:
        }
        var res int
        controlSocket, res, controlStopChan = self.dialAppControl()
        if res == 0 {
            fmt.Printf("Connected to control port\n")
            break
        }
        time.Sleep( time.Second * 10 )
        failures = failures + 1
        if failures >= 1 {
            fmt.Printf("Failed to connect video app control 5 times. Giving up.")
            return nil,true,nil
        }
    }
    return controlSocket,false,controlStopChan
}

func (self *AppStream) openVideo() (mangos.Socket,bool,chan bool) {
    var imgSocket mangos.Socket
    var vidStopChan chan bool
    fmt.Printf("Attempting to connect to video\n")
    failures := 0
    for {
        select {
            case <- self.stopChan: return nil,true,nil
            default:
        }
        var res int
        imgSocket, res, vidStopChan = self.dialAppVideo()
        if res == 0 { break }
        time.Sleep( time.Second * 1 )
        failures = failures + 1
        if failures >= 1 {
            fmt.Printf("Failed to connect video app stream 5 times. Giving up.")
            return nil,true,nil
        }
    }
    fmt.Printf("Connected to video port\n")
    return imgSocket,false,vidStopChan
}

func (self *AppStream) mainLoop() {
    go func() {
        //var controlSocket mangos.Socket
        var imgSocket     mangos.Socket
        var controlStopChan chan bool
        var vidStopChan     chan bool
        
        //imgHandler := NewImgHandler( self.stopChan, self.udid )
        self.imgHandler.setEnableStream( func() {
            self.controlSocket.Send([]byte(`{"action": "start"}`))
        } )
        self.imgHandler.setDisableStream( func() {
            self.controlSocket.Send([]byte(`{"action": "stop"}`))
        } )
        
        for {
            var done bool
            if self.controlSocket == nil {
                self.controlSocket,done,controlStopChan = self.openControl()
                if done { break }
            }
            if imgSocket == nil {
                imgSocket,done,vidStopChan = self.openVideo()
                if done { break }
            }
            
            self.imgHandler.setSource( imgSocket )
            res := self.imgHandler.mainLoop( vidStopChan, controlStopChan )
            if res == 1 { break } // stopChan
            if res == 2 { imgSocket = nil } // imgSocket connection lost
            if res == 3 { self.controlSocket = nil } // controlSocket connection lost
            if res == 4 { // lost send socket
                // TODO: Reconnect send socket
            } 
        }
        
        if self.controlSocket != nil { self.controlSocket.Close() }
        if imgSocket          != nil { imgSocket.Close() }
    }()
}

func ( self *AppStream) dialAppVideo() ( mangos.Socket, int, chan bool ) {
    vidSpec := self.vidSpec
    
    var err error
    var pullSock mangos.Socket
    
    if pullSock, err = nanoPull.NewSocket(); err != nil {
        log.WithFields( log.Fields{
            "type":     "err_socket_new",
            "zmq_spec": vidSpec,
            "err":      err,
        } ).Info("Socket new error")
        return nil, 1, nil
    }
    
    sec1, _ := time.ParseDuration( "1s" )
    setError := pullSock.SetOption( mangos.OptionRecvDeadline, sec1 )
    if setError != nil {
        fmt.Printf("Set timeout error %s\n", setError )
        os.Exit(0)
    }
    
    if err = pullSock.Dial(vidSpec); err != nil {
        log.WithFields( log.Fields{
            "type": "err_socket_dial",
            "spec": vidSpec,
            "err":  err,
        } ).Info("Socket dial error")
        return nil, 2, nil
    }
    
    vidStopChan := make( chan bool )
    
    pullSock.SetPipeEventHook( func( action mangos.PipeEvent, pipe mangos.Pipe ) {
        fmt.Printf("Pipe action %d\n", action )
        if action == 2 { vidStopChan <- true }
    } )
    
    return pullSock, 0, vidStopChan
}

func (self *AppStream) dialAppControl() ( mangos.Socket, int, chan bool ) {
    controlSpec := self.controlSpec
    
    var err error
    var reqSock mangos.Socket
    
    if reqSock, err = nanoReq.NewSocket(); err != nil {
        log.WithFields( log.Fields{
            "type":     "err_socket_new",
            "zmq_spec": controlSpec,
            "err":      err,
        } ).Info("Socket new error")
        return nil, 1, nil
    }
    
    sec1, _ := time.ParseDuration( "1s" )
    reqSock.SetOption( mangos.OptionRecvDeadline, sec1 )
    if err = reqSock.Dial( controlSpec ); err != nil {
        log.WithFields( log.Fields{
            "type": "err_socket_dial",
            "spec": controlSpec,
            "err":  err,
        } ).Info("Socket dial error")
        return nil, 2, nil
    }
    
    controlStopChan := make( chan bool )
    
    reqSock.SetPipeEventHook( func( action mangos.PipeEvent, pipe mangos.Pipe ) {
        fmt.Printf("Pipe action %d\n", action )
        if action == 2 { controlStopChan <- true }
    } )
    
    return reqSock, 0, controlStopChan
}

