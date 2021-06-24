package main

import (
    "fmt"
    "strings"

    "go.nanomsg.org/mangos/v3"
    
    // register transports
    _ "go.nanomsg.org/mangos/v3/transport/all"
    
    uj "github.com/nanoscopic/ujsonin/v2/mod"
)

type ImgHandler struct {
    inSock      mangos.Socket
    stopChan    chan bool
    imgConsumer *ImageConsumer
    mainCh      chan int
    discard     bool
    imgNum      int
    sentSize    bool
    enableStream  func()
    disableStream func()
    udid          string
    device      *Device
    isUp        bool
}

func NewImgHandler( stopChan chan bool, udid string, device *Device ) ( *ImgHandler ) {
    self := ImgHandler {
        inSock:      nil,
        stopChan:    stopChan,
        mainCh:      make( chan int ),
        discard:     true,
        imgNum:      1,
        udid:        udid,
        device:      device,
        isUp:        false,
    }
    return &self
}

func ( self *ImgHandler ) setImageConsumer( imgConsumer *ImageConsumer ) {
    self.imgConsumer = imgConsumer
}

func ( self *ImgHandler ) setEnableStream( enableStream func() ) {
    self.enableStream = enableStream
}

func ( self *ImgHandler ) setDisableStream( disableStream func() ) {
    self.disableStream = disableStream
}

func ( self *ImgHandler ) setSource( socket mangos.Socket ) {
    self.inSock = socket
}

func ( self *ImgHandler ) processImgMsg() (int) {
    msg, err := self.inSock.RecvMsg()
    
    if err != nil {
        if err == mangos.ErrRecvTimeout {
            return 2
        } else if err == mangos.ErrClosed {
            fmt.Printf("Connection to video closed\n")
            return 1
        } else {
            fmt.Printf( "Other error: %s", err )
            return 0
        }
    }
    
    self.imgNum = self.imgNum + 1
    if ( self.imgNum % 30 ) == 0 {
        fmt.Printf("Got incoming frame %d\n", self.imgNum)
    }
    
    if self.discard && self.sentSize {
        msg.Free()
        return 0
    }
    
    text := ""
    data := []byte{}
    // image is prepended by some JSON metadata
    if msg.Body[0] == '{' {
        endi := strings.Index( string(msg.Body), "}" )
        root, left := uj.Parse( msg.Body )
        lenLeft := len( left )
        if ( len(msg.Body ) - lenLeft - 1 ) != endi {
            fmt.Printf( "size mistmatched what was parsed: %d != %d\n", endi, len( msg.Body ) - len(left) - 1 )
        }
        data = left
        
        if lenLeft < 10 {
            // it's just a text message
            msg := root.Get("msg").String()
            if msg == "noframes" {
                self.imgConsumer.noframes()
            }
            return 0
        }
        
        //ow := root.Get("ow").Int()
        //oh := root.Get("oh").Int()
        dw := root.Get("dw").Int()
        dh := root.Get("dh").Int()
        
        //fmt.Printf("ow=%d, oh=%d, dw=%d, dh=%d\n", ow, oh, dw, dh )
        
        text = fmt.Sprintf("Width: %d, Height: %d, Size: %d\n", dw, dh, len( msg.Body ) )
        
        if !self.isUp {
            self.isUp = true
            self.device.EventCh <- DevEvent{ action: DEV_VIDEO_START }
        }
        
        if !self.sentSize {
            json := fmt.Sprintf( `{"type":"frame1","width":%d,"height":%d,"uuid":"%s"}`, dw, dh, self.udid ) 
            fmt.Printf("FIRSTFRAME%s\n",json)
            self.sentSize = true
        }
    } else {
        data = msg.Body
    }
    
    if !self.discard {
        err := self.imgConsumer.consume( text, data )
        msg.Free()
        if err != nil {
            // might as well begin discarding since we can't send
            self.discard = true
            return 3
        }
    } else {
        msg.Free()
    }

    return 0
}

func ( self *ImgHandler ) mainLoop( vidStopChan chan bool, controlStopChan chan bool) (int) {
    self.enableStream()
    var res int
    
    fmt.Printf("Main loop start\n")
    for {
        select {
            case <- controlStopChan:
                fmt.Printf("Lost connection to control socket\n")
                res = 3
                goto DONE
            case <- vidStopChan:
                fmt.Printf("Lost connection to video stream\n")
                self.isUp = false
                self.device.EventCh <- DevEvent{ action: DEV_VIDEO_STOP }
                res = 2
                goto DONE
            case <- self.stopChan:
                fmt.Printf("Server channel got stop message\n")
                res = 1
                goto DONE
            case msg := <- self.mainCh:
                if msg == 1 {
                    fmt.Printf("Setting discard to false\n")
                    self.discard = false
                } 
                if msg == 2 {
                    fmt.Printf("Setting discard to true\n")
                    self.discard = true
                } 
            default: // this makes the above read from stopChannel non-blocking
        }
        pres := self.processImgMsg()
        if pres == 1 {
            res = 2
            goto DONE
        }
        if pres == 3 { // lost send socket
            res = 4
            goto DONE
        }
        self.imgNum++
    }
    
    DONE:
    fmt.Printf("Main loop stop\n")
    self.disableStream()
    return res
}