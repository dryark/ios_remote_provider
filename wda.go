package main

import (
    "fmt"
    //"io/ioutil"
    "net/http"
    "strings"
    "time"
    log "github.com/sirupsen/logrus"
    uj "github.com/nanoscopic/ujsonin/v2/mod"
    "go.nanomsg.org/mangos/v3"
    nanoReq  "go.nanomsg.org/mangos/v3/protocol/req"
)

type WDA struct {
    udid          string
    onDevicePort  int
    localhostPort int
    mjpegPort     int
    devTracker    *DeviceTracker
    dev           *Device
    wdaProc       *GenericProc
    config        *Config
    base          string
    sessionId     string
    startChan     chan bool
    js2hid        map[int]int
    transport     *http.Transport
    client        *http.Client
    nngPort       int
    nngSocket     mangos.Socket
    disableUpdate bool
}

func NewWDA( config *Config, devTracker *DeviceTracker, dev *Device ) (*WDA) {
    self := NewWDANoStart( config, devTracker, dev )
    if config.wdaMethod != "manual" {
        self.start()
    } else {
        self.startWdaNng(nil)
    }
    return self
}

func addrange( amap map[int]int, from1 int, to1 int, from2 int ) {
    for i:=from1; i<=to1; i++ {
        amap[ i ] = i - from1 + from2
    }
}

func NewWDANoStart( config *Config, devTracker *DeviceTracker, dev *Device ) (*WDA) {
    jh := make( map[int]int )  
  
    self := WDA{
        udid:          dev.udid,
        onDevicePort:  8100,
        localhostPort: dev.wdaPort,
        nngPort:       dev.wdaNngPort,
        mjpegPort:     dev.mjpegVideoPort,
        devTracker:    devTracker,
        dev:           dev,
        config:        config,
        base:          fmt.Sprintf("http://127.0.0.1:%d",dev.wdaPort),
        js2hid:        jh,
        transport:     &http.Transport{},
    }
    self.client = &http.Client{
      Transport: self.transport,
    }
    
    /*
    The following generates a map of "JS keycodes" to Apple IO Hid event numbers.
    At least, for everything accessible without using Shift...
    At least for US keyboards.
    
    TODO: Modify this to read in information from configuration and also pay attention
    to the region of the device. In other regions keyboards will have different keys.
    
    The positive numbers here are the normal character set codes; in this case they are
    ASCII.
    
    The negative numbers are JS keyCodes for non-printable characters.
    */
    addrange( jh, 97, 122, 4 ) // a-z
    addrange( jh, 49, 57, 0x1e ) // 1-9
    jh[32] = 0x2c // space
    jh[39] = 0x34 // '
    jh[44] = 0x36 // ,
    jh[45] = 0x2d // -
    jh[46] = 0x37 // .
    jh[47] = 0x38 // /
    jh[48] = 0x27 // 0
    jh[59] = 0x33 // ;
    jh[61] = 0x2e // =
    jh[91] = 0x2f // [
    jh[92] = 0x31 // \
    jh[93] = 0x30 // ]
    //jh[96] = // `
    
    jh[-8] = 0x2a // backspace
    jh[-9] = 0x2b // tab
    jh[-13] = 0x28 // enter
    jh[-27] = 0x29 // esc
    jh[-33] = 0x4b // pageup
    jh[-34] = 0x4e // pagedown
    jh[-35] = 0x4d // end
    jh[-36] = 0x4a // home
    
    jh[-37] = 0x50 // left
    jh[-38] = 0x52 // up
    jh[-39] = 0x4f // right
    jh[-40] = 0x51 // down
    jh[-46] = 0x4c // delete
      
    return &self
}

func (self *WDA) dialWdaNng() ( mangos.Socket, int, chan bool ) {
    spec := fmt.Sprintf( "tcp://127.0.0.1:%d", self.nngPort )
    
    var err error
    var reqSock mangos.Socket
    
    if reqSock, err = nanoReq.NewSocket(); err != nil {
        log.WithFields( log.Fields{
            "type":     "err_socket_new",
            "zmq_spec": spec,
            "err":      err,
        } ).Info("Socket new error")
        return nil, 1, nil
    }
    
    /*sec1, _ := time.ParseDuration( "5s" )
    reqSock.SetOption( mangos.OptionRecvDeadline, sec1 )*/
    
    if err = reqSock.Dial( spec ); err != nil {
        log.WithFields( log.Fields{
            "type": "err_socket_dial",
            "spec": spec,
            "err":  err,
        } ).Info("Socket dial error")
        return nil, 2, nil
    }
    
    stopChan := make( chan bool )
    
    reqSock.SetPipeEventHook( func( action mangos.PipeEvent, pipe mangos.Pipe ) {
        fmt.Printf("Pipe action %d\n", action )
        if action == 2 { stopChan <- true }
    } )
    
    return reqSock, 0, stopChan
}

func (self *WDA) startWdaNng( onready func() ) {
    pairs := []TunPair{
        TunPair{ from: self.localhostPort, to: self.onDevicePort },
        //TunPair{ from: self.mjpegPort,     to: 8150 },
        TunPair{ from: self.nngPort,       to: 8101 },
    }
    
    self.dev.bridge.tunnel( pairs, func() {
        fmt.Printf("Starting NNG\n")
        nngSocket, _, _ := self.dialWdaNng()
        self.nngSocket = nngSocket
        fmt.Printf("NNG Started\n")
        self.create_session("")
        if onready != nil {
            onready()            
        }
    } )
}

func (self *WDA) start() {
    pairs := []TunPair{
        TunPair{ from: self.localhostPort, to: self.onDevicePort },
        TunPair{ from: self.mjpegPort,     to: 8150 },
        TunPair{ from: self.nngPort,       to: 8101 },
    }
    
    self.dev.bridge.tunnel( pairs, func() {
        self.dev.bridge.wda(
            self.localhostPort,
            func() { // onStart
                log.WithFields( log.Fields{
                    "type": "wda_start",
                    "udid":  censorUuid(self.udid),
                    "port": self.localhostPort,
                    "mjpegPort": self.mjpegPort,
                } ).Info("[WDA] successfully started")
                
                if self.startChan != nil {
                    self.startChan <- true
                }
                self.dev.EventCh <- DevEvent{ action: DEV_WDA_START }
                
                fmt.Printf("Starting NNG\n")
                nngSocket, _, _ := self.dialWdaNng()
                self.nngSocket = nngSocket
                fmt.Printf("NNG Started\n")
                
            },
            func(interface{}) { // onStop
                self.dev.EventCh <- DevEvent{ action: DEV_WDA_STOP }
            },
            8150,
        )
    } )
}

func (self *WDA) stop() {
    if self.wdaProc != nil {
        self.wdaProc.Kill()
        self.wdaProc = nil
    }
}

func (self *WDA) ensureSession() {
    sid := self.get_session()
    if sid == "" {
        //fmt.Printf("No WDA session exists. Creating\n" )
        sid = self.create_session( "" )
        //fmt.Printf("Created wda session id=%s\n", sid )
    } else {
        //fmt.Printf("Session existing; id=%s\n", sid )
    }
    self.sessionId = sid
}

func ( self *WDA ) get_session() ( string ) {
    self.nngSocket.Send([]byte(`{ action: "status" }`))
    jsonBytes, _ := self.nngSocket.Recv()
    root, _, _ := uj.ParseFull( jsonBytes )
    sessionIdNode := root.Get("sessionId")
    if sessionIdNode == nil {
        return ""
    }
    
    return sessionIdNode.String()
}

func ( self *WDA ) create_session( bundle string ) ( string ) {
    if bundle == "" {
        //bundle = "com.apple.Preferences"
    }
    
    fmt.Printf("Creating session; bi=%s\n", bundle )
    
    self.disableUpdate = true
    
    json := fmt.Sprintf( `{
      action: "createSession"
      bundleId: "%s"
    }`, bundle )
        
    err := self.nngSocket.Send([]byte(json))
    if err != nil {
        fmt.Printf("Send error: %s\n", err )
    }
    fmt.Printf("Sent; receiving\n" )
    
    sessionIdBytes, err := self.nngSocket.Recv()
    if err != nil {
        fmt.Printf( "sessionCreate err: %s\n", err )
    }
    
    self.disableUpdate = false
    
    sessionId := string( sessionIdBytes )
    
    fmt.Printf("Created session; id=%s\n", sessionId )
    
    return sessionId
}

func (self *WDA) clickAt( x int, y int ) {
    json := fmt.Sprintf( `{
        action: "tap"
        x:%d
        y:%d
    }`, x, y )
    
    self.nngSocket.Send([]byte(json))
    self.nngSocket.Recv()
}

func (self *WDA) hardPress( x int, y int ) {
    log.Info( "Hard Press:", x, y )
    json := fmt.Sprintf( `{
        action: "touchPerform"
        "actions":[
            {
              "action": "press",
              "options": {
                "x":%d,
                "y":%d,
                "pressure":3000
              }
            },
            {
              "action":"wait",
              "options": {
                "ms": 700
              }
            },
            {
              "action":"release",
              "options":{}
            }
        ]
    }`, x, y )
    
    self.nngSocket.Send([]byte(json))
    self.nngSocket.Recv()
}

func (self *WDA) longPress( x int, y int ) {
    log.Info( "Long Press:", x, y )
    json := fmt.Sprintf( `{
    action: "touchPerform"
    "actions": [
      {
        "action": "press",
        "options": {
          "x":%d,
          "y":%d
        }
      },
      {
        "action":"wait",
        "options": {
          "ms": 500
        }
      },
      {
        "action":"release",
        "options":{}
      }
    ]
    }`, x, y )
    
    self.nngSocket.Send([]byte(json))
    self.nngSocket.Recv()
}

func (self *WDA) home() (string) {
    json := `{
      action: "button"
      name: "home"
      duration: 5
    }`
    self.nngSocket.Send([]byte(json))
    self.nngSocket.Recv()
    
    return ""
}

func (self *WDA) keys( codes []int ) {
    if len( codes ) > 1 {
        self.typeText( codes )
        return
    }
    code := codes[0]
    
    /*
    Only some keys are able to be pressed via IoHid, because I cannot
    figure out how to use 'shift' accessed characters/keys through it.
    
    If someone is able to figure it out please let me know as the "ViaKeys"
    method uses the much slower [application typeType] method.
    */
    if self.config.wdaKeyMethod == "iohid" {
        dest, ok := self.js2hid[ code ]
        if ok {
            self.keysViaIohid( []int{dest} )
        } else {
            self.typeText( codes )
        }
    } else {
        self.typeText( codes )
    }
}

func (self *WDA) keysViaIohid( codes []int ) {
    /*
    This loop of making repeated calls is obviously quite garbage.
    A better solution would be to make a call in WDA itself able to handle
    multiple characters at once.
    
    Despite this the performIoHidEvent call is very fast so it can generally
    keep up with typing speed of a manual user of CF.
    */
    for _, code := range codes {
        json := fmt.Sprintf(`{
          action: "iohid"
          page: 7
          usage: %d
          duration: 0.05
        }`, code )
        
        log.Info( "sending " + json )
        
        self.nngSocket.Send([]byte(json))
        self.nngSocket.Recv()
    }
}

func (self *WDA) ioHid( page int, code int ) {
    json := fmt.Sprintf(`{
      action: "iohid"
      page: %d
      usage: %d
      duration: 0.05
    }`, page, code )
        
    log.Info( "sending " + json )
        
    self.nngSocket.Send([]byte(json))
    self.nngSocket.Recv()
}

func (self *WDA) typeText( codes []int ) {
    strArr := []string{}

    for _, code := range codes {
        // GoLang encodes to utf8 by default. typeText call expects utf8 encoding
        strArr = append( strArr, fmt.Sprintf("%c", rune( code ) ) )
    }
    
    json := fmt.Sprintf(`{
        action: "typeText"
        text: "%s"
    }`, strings.Join( strArr, "" ) )
   
    log.Info( "sending " + json )
      
    self.nngSocket.Send([]byte(json))
    self.nngSocket.Recv()
}

func ( self *WDA ) swipe( x1 int, y1 int, x2 int, y2 int, delay float64 ) {
    log.Info( "Swiping:", x1, y1, x2, y2, delay )
    
    json := fmt.Sprintf( `{
        action: "swipe"
        x1:%d
        y1:%d
        x2:%d
        y2:%d
        delay:%.2f
    }`, x1, y1, x2, y2, delay )
    
    self.nngSocket.Send([]byte(json))
    self.nngSocket.Recv()
}

func (self *WDA) ElClick( elId string ) {
    json := fmt.Sprintf( `{
        action: "elClick"
        id: "%s"
    }`, elId )
    
    self.nngSocket.Send([]byte(json))
    self.nngSocket.Recv()
}

func (self *WDA) ElForceTouch( elId string, pressure int ) {
    json := fmt.Sprintf( `{
        action: "elForceTouch"
        element: "%s"
        "duration": 1
        "pressure": %d
    }`, elId, pressure )
    
    self.nngSocket.Send([]byte(json))
    self.nngSocket.Recv()
}

func (self *WDA) ElLongTouch( elId string ) {
    json := fmt.Sprintf( `{
        action: "elTouchAndHold"
        element: "%s"
        duration: 2
    }`, elId )
    
    self.nngSocket.Send([]byte(json))
    self.nngSocket.Recv()
}

func (self *WDA) ElByName( elName string ) string {
    json := fmt.Sprintf( `{
        action: "elByName"
        name: "%s"
        sessionId: "%s"
    }`, elName, self.sessionId )
    
    self.nngSocket.Send([]byte(json))
    idBytes, _ := self.nngSocket.Recv()
    
    return string( idBytes )
}

func (self *WDA) WindowSize() (int,int) {
    self.nngSocket.Send([]byte(`{ action: "windowSize" }`))
    jsonBytes, _ := self.nngSocket.Recv()
    root, _, _ := uj.ParseFull( jsonBytes )
    width := root.Get("width").Int()
    height := root.Get("height").Int()
    
    return width,height
}

func (self *WDA) Source() string {
    self.nngSocket.Send([]byte(`{ action: "source" }`))
    srcBytes, _ := self.nngSocket.Recv()
        
    return string(srcBytes)
}

func (self *WDA) StartBroadcastStream( appName string, bid string ) {
    sid := self.create_session( bid )
    self.sessionId = sid
    
    time.Sleep( time.Second * 4 )
    
    toSelector := self.ElByName( "Broadcast Selector" )
    self.ElClick( toSelector )
    
    time.Sleep( time.Second * 2 )
    //self.Source()
    
    startBtn := self.ElByName( "Start Broadcast" )
    self.ElClick( startBtn )
    
    time.Sleep( time.Second * 5 )
}

func (self *WDA) AppChanged( bundleId string ) {
    if self.disableUpdate { return }
    
    json := fmt.Sprintf( `{
        action: "updateApplication"
        bundleId: "%s"
    }`, bundleId )
    
    self.nngSocket.Send([]byte(json))
    self.nngSocket.Recv()
}