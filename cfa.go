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

type CFA struct {
    udid          string
    devTracker    *DeviceTracker
    dev           *Device
    cfaProc       *GenericProc
    config        *Config
    base          string
    //sessionId     string
    startChan     chan int
    js2hid        map[int]int
    transport     *http.Transport
    client        *http.Client
    nngPort       int
    nngSocket     mangos.Socket
    disableUpdate bool
    sessionMade   bool
}

func NewCFA( config *Config, devTracker *DeviceTracker, dev *Device ) (*CFA) {
    self := NewCFANoStart( config, devTracker, dev )
    if config.cfaMethod != "manual" {
        self.start( nil )
    } else {
        self.startCfaNng( func( err int, stopChan chan bool ) {
            if err != 0 {
                dev.EventCh <- DevEvent{ action: DEV_CFA_START_ERR }
            } else {
                dev.EventCh <- DevEvent{ action: DEV_CFA_START }
            }
        } )
    }
    return self
}

func addrange( amap map[int]int, from1 int, to1 int, from2 int ) {
    for i:=from1; i<=to1; i++ {
        amap[ i ] = i - from1 + from2
    }
}

func NewCFANoStart( config *Config, devTracker *DeviceTracker, dev *Device ) (*CFA) {
    jh := make( map[int]int )  
  
    self := CFA{
        udid:          dev.udid,
        nngPort:       dev.cfaNngPort,
        devTracker:    devTracker,
        dev:           dev,
        config:        config,
        //base:          fmt.Sprintf("http://127.0.0.1:%d",dev.wdaPort),
        js2hid:        jh,
        transport:     &http.Transport{},
    }
    //self.client = &http.Client{
    //    Transport: self.transport,
    //}
    
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

func (self *CFA) dialCfaNng() ( mangos.Socket, int, chan bool ) {
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
        //fmt.Printf("Pipe action %d\n", action )
        if action == 2 {
            stopChan <- true
        }
    } )
    
    return reqSock, 0, stopChan
}

func (self *CFA) startCfaNng( onready func( int, chan bool ) ) {
    pairs := []TunPair{
        TunPair{ from: self.nngPort, to: 8101 },
    }
    
    self.dev.bridge.tunnel( pairs, func() {
        nngSocket, err, stopChan := self.dialCfaNng()
        if err != 0 {
            onready( err, nil )
            return
        }
        self.nngSocket = nngSocket
        self.create_session("")
        if onready != nil {
            onready( 0, stopChan )            
        }
    } )
}

func (self *CFA) start( started func( int, chan bool ) ) {
    pairs := []TunPair{
        TunPair{ from: self.nngPort, to: 8101 },
    }
    
    self.dev.bridge.tunnel( pairs, func() {
        self.dev.bridge.cfa(
            func() { // onStart
                log.WithFields( log.Fields{
                    "type": "cfa_start",
                    "udid":  censorUuid(self.udid),
                    "nngPort": self.nngPort,
                } ).Info("[CFA] successfully started")
                
                log.WithFields( log.Fields{
                    "type": "cfa_nng_dialing",
                    "port": self.nngPort,
                } ).Debug("CFA - Dialing NNG")
                
                nngSocket, err, stopChan := self.dialCfaNng()
                if err == 0 {
                    self.nngSocket = nngSocket
                    log.WithFields( log.Fields{
                        "type": "cfa_nng_dialed",
                        "port": self.nngPort,
                    } ).Debug("WDA - NNG Dialed")
                } else {
                    fmt.Printf("Error starting/connecting to CFA.\n")
                    self.dev.EventCh <- DevEvent{ action: DEV_CFA_START_ERR }
                    return
                }
                
                if started != nil {
                    started( 0, stopChan )
                }
                
                if self.startChan != nil {
                    self.startChan <- 0
                }
                
                self.dev.EventCh <- DevEvent{ action: DEV_CFA_START }
            },
            func(interface{}) { // onStop
                self.dev.EventCh <- DevEvent{ action: DEV_CFA_STOP }
            },
        )
    } )
}

func (self *CFA) stop() {
    if self.cfaProc != nil {
        self.cfaProc.Kill()
        self.cfaProc = nil
    }
}

func (self *CFA) ensureSession() {
    sid := self.get_session()
    if sid == "" {
        //fmt.Printf("No CFA session exists. Creating\n" )
        sid = self.create_session( "" )
        //fmt.Printf("Created cfa session id=%s\n", sid )
    } else {
        //fmt.Printf("Session existing; id=%s\n", sid )
    }
}

func ( self *CFA ) get_session() ( string ) {
    if self.sessionMade {
        return "1"
    } else {
        return ""
    }
}

func ( self *CFA ) create_session( bundle string ) ( string ) {
    if bundle == "" {
        //bundle = "com.apple.Preferences"
        log.WithFields( log.Fields{
            "type": "cfa_session_creating",
            "bi": "NONE",
        } ).Debug("Creating CFA session")
    } else {
        log.WithFields( log.Fields{
            "type": "cfa_session_creating",
            "bi": bundle,
        } ).Debug("Creating CFA session")
    }
    
    self.disableUpdate = true
    
    json := fmt.Sprintf( `{
        action: "createSession"
        bundleId: "%s"
    }`, bundle )
        
    err := self.nngSocket.Send([]byte(json))
    if err != nil {
        fmt.Printf("Send error: %s\n", err )
    }
    //fmt.Printf("Sent; receiving\n" )
    
    _, err = self.nngSocket.Recv()
    sid := ""
    if err != nil {
        fmt.Printf( "sessionCreate err: %s\n", err )
    } else {
        sid = "1"
        self.sessionMade = true
    }    
    
    self.disableUpdate = false
    
    log.WithFields( log.Fields{
        "type": "cfa_session_created",
    } ).Info("Created CFA session")
    
    return sid
}

func (self *CFA) clickAt( x int, y int ) {
    json := fmt.Sprintf( `{
        action: "tap"
        x:%d
        y:%d
    }`, x, y )
    
    self.nngSocket.Send([]byte(json))
    self.nngSocket.Recv()
}

func (self *CFA) hardPress( x int, y int ) {
    log.Info( "Firm Press:", x, y )
    json := fmt.Sprintf( `{
        action: "tapFirm"
        x:%d
        y:%d
        pressure:3000
    }`, x, y )
    
    self.nngSocket.Send([]byte(json))
    self.nngSocket.Recv()
}

func (self *CFA) longPress( x int, y int ) {
    log.Info( "Press for time:", x, y, 1.0 )
    json := fmt.Sprintf( `{
        action: "tapTime"
        x:%d
        y:%d
        time:1.0
    }`, x, y )
    
    self.nngSocket.Send([]byte(json))
    self.nngSocket.Recv()
}

func (self *CFA) home() (string) {
    json := `{
      action: "button"
      name: "home"
      duration: 5
    }`
    self.nngSocket.Send([]byte(json))
    self.nngSocket.Recv()
    
    return ""
}

func (self *CFA) keys( codes []int ) {
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
    if self.config.cfaKeyMethod == "iohid" {
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

func (self *CFA) keysViaIohid( codes []int ) {
    /*
    This loop of making repeated calls is obviously quite garbage.
    A better solution would be to make a call in CFA itself able to handle
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

func (self *CFA) ioHid( page int, code int ) {
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

func (self *CFA) typeText( codes []int ) {
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

func ( self *CFA ) swipe( x1 int, y1 int, x2 int, y2 int, delay float64 ) {
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

func (self *CFA) ElClick( elId string ) {
    log.Info( "elClick:", elId )
    json := fmt.Sprintf( `{
        action: "elClick"
        id: "%s"
    }`, elId )
    
    self.nngSocket.Send([]byte(json))
    self.nngSocket.Recv()
}

func (self *CFA) ElForceTouch( elId string, pressure int ) {
    log.Info( "elForceTouch:", elId, pressure )
    json := fmt.Sprintf( `{
        action: "elForceTouch"
        element: "%s"
        "duration": 1
        "pressure": %d
    }`, elId, pressure )
    
    self.nngSocket.Send([]byte(json))
    self.nngSocket.Recv()
}

func (self *CFA) ElLongTouch( elId string ) {
    log.Info( "elTouchAndHold", elId )
    json := fmt.Sprintf( `{
        action: "elTouchAndHold"
        id: "%s"
        time: 2.0
    }`, elId )
    
    self.nngSocket.Send([]byte(json))
    self.nngSocket.Recv()
}

func (self *CFA) GetEl( elType string, elName string, system bool, wait int ) string {
    log.Info( "getEl:", elName )
    
    sysLine := ""
    if system {
        sysLine = "system:1";
    }
    
    waitLine := ""
    if wait > 0 {
        waitLine = fmt.Sprintf("wait:%d",wait)
    }
    
    json := fmt.Sprintf( `{
        action: "getEl"
        type: "%s"
        id: "%s"
        %s
        %s
    }`, elType, elName, sysLine, waitLine )
    
    self.nngSocket.Send([]byte(json))
    idBytes, _ := self.nngSocket.Recv()
    
    log.Info( "getEl-result:", string(idBytes) )
    
    return string( idBytes )
}

func (self *CFA) WindowSize() (int,int) {
    log.Info("windowSize")
    self.nngSocket.Send([]byte(`{ action: "windowSize" }`))
    jsonBytes, _ := self.nngSocket.Recv()
    root, _, _ := uj.ParseFull( jsonBytes )
    width := root.Get("width").Int()
    height := root.Get("height").Int()
    
    log.Info("windowSize-result:",width,height)
    return width,height
}

func (self *CFA) Source() string {
    self.nngSocket.Send([]byte(`{ action: "source" }`))
    srcBytes, _ := self.nngSocket.Recv()
        
    return string(srcBytes)
}

func (self *CFA) AlertInfo() ( uj.JNode, string ) {
    self.nngSocket.Send([]byte(`{ action: "alertInfo" }`))
    jsonBytes, _ := self.nngSocket.Recv()
    fmt.Printf("alertInfo res: %s\n", string(jsonBytes) )
    root, _, _ := uj.ParseFull( jsonBytes )
    presentNode := root.Get("present")
    if presentNode == nil {
        fmt.Printf("Error reading alertInfo; got back %s\n", string(jsonBytes) )
        return nil, string(jsonBytes)
    } else {
        if presentNode.Bool() == false { return nil, string(jsonBytes) }
        return root, string(jsonBytes)
    }
}

func (self *CFA) SourceJson() string {
    self.nngSocket.Send([]byte(`{ action: "sourcej" }`))
    srcBytes, _ := self.nngSocket.Recv()
        
    return string(srcBytes)
}

func (self *CFA) IsLocked() bool {
    self.nngSocket.Send([]byte(`{ action: "isLocked" }`))
    jsonBytes, _ := self.nngSocket.Recv()
    root, _, _ := uj.ParseFull( jsonBytes )
    return root.Get("locked").Bool()
}

func (self *CFA) Unlock () {
    self.nngSocket.Send([]byte(`{ action: "unlock" }`))
    res, _ := self.nngSocket.Recv()
    fmt.Printf("Result:%s\n", string( res ) )
}

func (self *CFA) OpenControlCenter( controlCenterMethod string ) {
    fmt.Printf("Opening control center\n")  
    width, height := self.WindowSize()
    
    if controlCenterMethod == "bottomUp" {
        midx := width / 2
        maxy := height - 1
        self.swipe( midx, maxy, midx, maxy - 100, 0.1 )
    } else if controlCenterMethod == "topDown" {
        maxx := width - 1
        self.swipe( maxx, 0, maxx, 100, 0.1 )
    }    
}

func (self *CFA) StartBroadcastStream( appName string, bid string, devConfig *CDevice ) {
    method := devConfig.vidStartMethod
    ccMethod := devConfig.controlCenterMethod
    
    sid := self.create_session( bid )
    if sid == "" {
        // TODO error creating session
    }
    
    fmt.Printf("Checking for alerts\n")
    alerts := self.config.vidAlerts
    for {
        alert, _ := self.AlertInfo()
        if alert == nil { break }
        text := alert.Get("alert").String()
        
        dismissed := false
        // dismiss the alert
        for _, alert := range alerts {
            if strings.Contains( text, alert.match ) {
                fmt.Printf("Alert matching \"%s\" appeared. Autoresponding with \"%s\"\n",
                    alert.match, alert.response )
                btn := self.GetEl( "button", alert.response, true, 0 )
                if btn == "" {
                    fmt.Printf("Alert does not contain button \"%s\"\n", alert.response )
                } else {
                    self.ElClick( btn )
                    dismissed = true
                    break
                }
            }
        }
        if !dismissed {
            // TODO; get rid of the alert some other way
            break
        }
        
        // Give time for another alert to appear
        time.Sleep( time.Second * 1 )
    }
   
    fmt.Printf("vidApp start method: %s\n", method )
    if method == "app" {
        fmt.Printf("Starting vidApp through the app\n")
        
        toSelector := self.GetEl( "button", "Broadcast Selector", false, 5 )
        self.ElClick( toSelector )
        
        startBtn := self.GetEl( "button", "Start Broadcast", true, 5 )
        self.ElClick( startBtn )
    } else if method == "controlCenter" {
        fmt.Printf("Starting vidApp through control center\n")
        self.OpenControlCenter( ccMethod )
        //self.Source()
        
        devEl := self.GetEl( "button", "Screen Recording", true, 5 )
        fmt.Printf("Selecting Screen Recording; el=%s\n", devEl )
        self.ElLongTouch( devEl )
        
        appEl := self.GetEl( "any", appName, true, 5 )
        self.ElClick( appEl )
        
        startBtn := self.GetEl( "button", "Start Broadcast", true, 5 )
        self.ElClick( startBtn )
        
        time.Sleep( time.Second * 3 )
    } else if method == "manual" {
    }
        
    time.Sleep( time.Second * 5 )
}

func (self *CFA) AppChanged( bundleId string ) {
    if self.disableUpdate { return }
    
    json := fmt.Sprintf( `{
        action: "updateApplication"
        bundleId: "%s"
    }`, bundleId )
    
    self.nngSocket.Send([]byte(json))
    self.nngSocket.Recv()
}