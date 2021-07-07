package main

import (
    "fmt"
    "io/ioutil"
    "net/http"
    "strings"
    "time"
    log "github.com/sirupsen/logrus"
    uj "github.com/nanoscopic/ujsonin/v2/mod"
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
}

func NewWDA( config *Config, devTracker *DeviceTracker, dev *Device ) (*WDA) {
    self := NewWDANoStart( config, devTracker, dev )
    self.start()
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
        mjpegPort:     dev.mjpegVideoPort,
        devTracker:    devTracker,
        dev:           dev,
        config:        config,
        base:          fmt.Sprintf("http://127.0.0.1:%d",dev.wdaPort),
        js2hid:        jh,
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

func (self *WDA) start() {
    pairs := []TunPair{
        TunPair{ from: self.localhostPort, to: self.onDevicePort },
        TunPair{ from: self.mjpegPort,     to: 8150 },
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

func resp_to_str( resp *http.Response ) ( string ) {
    data, _ := ioutil.ReadAll( resp.Body )
    resp.Body.Close()
    return string(data)
}

func resp_to_val( resp *http.Response ) ( uj.JNode, string ) {
    if resp == nil {
        fmt.Printf("nil response from http request\n")
        return nil, ""
    }
    rawContent := resp_to_str( resp )
    if len( rawContent ) == 0 { return nil, "" }
    if !strings.HasPrefix( rawContent, "{" ) {
        return nil, rawContent // &JHash{ nodeType: 1, hash: NewNodeHash() }
    }
    content, _ := uj.Parse( []byte( rawContent ) )
    val := content.Get("value")
    if val == nil { return content, rawContent }
    return val, rawContent
}

func ( self *WDA ) get_session() ( string ) {
    resp, err := http.Get( self.base + "/status" )
    if err != nil {
        return ""
    }
    
    statText := resp_to_str( resp )
    //fmt.Printf("status text:%s\n", statText )
    
    stat, _ := uj.Parse( []byte( statText ) )
    
    //status := resp_to_val( resp )
    sessNode := stat.Get("sessionId")
    if sessNode == nil { return "" }
    return sessNode.String()
}

func ( self *WDA ) create_session( bundle string ) ( string ) {
    /*
    The blank session create does not work for all device types
    so it is disabled here. I wish WDA itself was documentd better
    as to how it is expected to work.
    */
    
    /*ops := fmt.Sprintf( `{
      "capabilities": {
        "alwaysMatch": {},
        "firstMatch": [
          {
            
          }
        ]
      }
    }` )*/
    
    if bundle == "" {
        bundle = "com.apple.Preferences"
    }
    
    ops := fmt.Sprintf( `{
      "capabilities": {
        "alwaysMatch": {
            "arguments": [],
            "bundleId": "%s",
            "environment": {},
            "shouldUseSingletonTestManager": true,
            "shouldUseTestManagerForVisibilityDetection": false,
            "shouldWaitForQuiescence": false
        },
        "firstMatch": [ { } ]
      }
    }`, bundle )
    
    //resp, _ := http.Get( self.base + "/health" )
    //hStr := resp_to_str( resp )
    //fmt.Printf("resp to health:%v\n", hStr )
    client := &http.Client{}
    
    var res uj.JNode
    rawRes := ""
    for {
        req, _ := http.NewRequest( "POST", self.base + "/session", strings.NewReader( ops ) )
        req.Close = true
        req.Header.Set("Content-Type", "application/json")
        resp, err := client.Do( req )
        
        /*
        There was an issue using this one line call. The Close = true was needed to address a bug
        Don't blindly simplify it back to this as it won't work correctly.
        */
        // resp, err := http.Post( self.base + "/session", "application/json", strings.NewReader( ops ) )
        if err != nil {
            errText := err.Error()
            fmt.Printf("Error creating session: %s\n", errText )
            fmt.Printf("  Retrying\n")
            time.Sleep( time.Second * 1 )
            continue
        }
        
        if resp.StatusCode != 200 {
            str := resp_to_str( resp )
            
            fmt.Printf("Got status %d back from query to %s\nstr = %s\n", resp.StatusCode, self.base + "/session", str )
            return ""
        }
        
        res, rawRes = resp_to_val( resp )
        if res == nil {
            fmt.Printf("Session create results:`%s`\n", rawRes )
            fmt.Printf("  Could not parse. Retrying")
            continue
        }
        break
    }
    
    sessNode := res.Get("sessionId")
    
    if sessNode == nil {
        fmt.Printf("Result of session create:%s\n", rawRes )
        panic("Did not get sessionId on session create")
    }
    return sessNode.String()
}

func (self *WDA) clickAt( x int, y int ) {
    /*json := fmt.Sprintf( `{
        "actions":[
            {
                "action":"tap",
                "options":{
                    "x":%d,
                    "y":%d
                }
            }
        ]
    }`, x, y )
    self.sessionCall( "/wda/touch/perform", json )*/
    json := fmt.Sprintf( `{
        "x":%d,
        "y":%d
    }`, x, y )
    http.Post( self.base + "/wda/tap", "application/json", strings.NewReader( json ) )
}

func (self *WDA) sessionCallGet( url string ) uj.JNode {
    var err uj.JNode
    var val uj.JNode
    
    if self.sessionId == "" {
        self.ensureSession()
    }
    
    fullUrl := self.base + "/session/" + self.sessionId + url
    fmt.Printf("Getting %s\n", fullUrl )
    
    resp, _ := http.Get( fullUrl )
    
    rawVal := ""
    val, rawVal = resp_to_val( resp )
    fmt.Printf("Result: %s\n", rawVal )
    err = val.Get("error")
    
    if err != nil {
        errText := err.String()
        if errText == "invalid session id" {
            fmt.Printf("Invalid session at first; repeating call\n")
            self.ensureSession()
            resp, _ := http.Get( self.base + "/session/" + self.sessionId + url )
            val, _ = resp_to_val( resp )
        }
    }
    
    return val
}

func (self *WDA) sessionCall( url string, json string ) uj.JNode {
    var err uj.JNode
    var val uj.JNode
    
    if self.sessionId == "" {
        self.ensureSession()
    }
    
    fullUrl := self.base + "/session/" + self.sessionId + url
    fmt.Printf("Posting to %s\n", fullUrl )
    
    resp, _ := http.Post(
        fullUrl,
        "application/json",
        strings.NewReader( json ),
    )
    
    rawVal := ""
    val, rawVal = resp_to_val( resp )
    fmt.Printf("Result: %s\n", rawVal )
    err = val.Get("error")
    
    if err != nil {
        errText := err.String()
        if errText == "invalid session id" {
            fmt.Printf("Invalid session at first; repeating call\n")
            self.ensureSession()
            resp, _ := http.Post(
                self.base + "/session/" + self.sessionId + url,
                "application/json",
                strings.NewReader( json ),
            )
            val, _ = resp_to_val( resp )
        }
    }
    
    return val
}

func (self *WDA) hardPress( x int, y int ) {
    log.Info( "Hard Press:", x, y )
    json := fmt.Sprintf( `{
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
    self.sessionCall( "/wda/touch/perform", json )
}

func (self *WDA) longPress( x int, y int ) {
    log.Info( "Long Press:", x, y )
    json := fmt.Sprintf( `{
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
    
    self.sessionCall( "/wda/touch/perform", json )
}

func (self *WDA) home() (string) {
    http.Post( self.base + "/wda/homescreen", "application/json", strings.NewReader( "{}" ) )
    return ""  
}

func (self *WDA) keys( codes []int ) {
    if len( codes ) > 1 {
        self.keysViaKeys( codes )
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
            self.keysViaKeys( codes )
        }
    } else {
        self.keysViaKeys( codes )
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
          "page": 7,
          "usage": %d,
          "duration": 0.05
        }`, code )
        
        log.Info( "sending " + json )
        
        http.Post( self.base + "/wda/performIoHidEvent", "application/json", strings.NewReader( json ) )
        //self.sessionCall( "/wda/performIoHidEvent", json )
    }
}

func (self *WDA) keysViaKeys( codes []int ) {
    strArr := []string{}
    /*
    I'm not sure what character set is used/accepted by WDA and in turn XCUITest for this
    call, so I'm turning everything except basic alpha characters into unicode hex notation for
    safety.
    
    Yet another case of the lack of WDA documentation being a problem.
    */
    for _, code := range codes {
        if code >= 97 && code <= 122 {
            strArr = append( strArr, fmt.Sprintf("%c", rune( code ) ) )
        } else {
            strArr = append( strArr, fmt.Sprintf("\\u%04x", code ) )
        }
    }
    
    json := fmt.Sprintf(`{
        "key": "%s"
    }`, strings.Join( strArr, "" ) )
    
    log.Info( "sending " + json )
    
    self.sessionCall( "/wda/keyAT", json )
}

func ( self *WDA ) swipe( x1 int, y1 int, x2 int, y2 int ) {
    log.Info( "Swiping:", x1, y1, x2, y2 )
    json := fmt.Sprintf( `{
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
        "action": "moveTo",
        "options": {
          "x":%d,
          "y":%d
        }
      },
      {
        "action":"release",
        "options":{}
      }
    ]
    }`, x1, y1, x2, y2 )
    
    self.sessionCall( "/wda/touch/perform", json )
}

func (self *WDA) ElClick( elId string ) {
    self.sessionCall( "/element/" + elId + "/click", "{}" )
}

func (self *WDA) ElForceTouch( elId string, pressure int ) {
    jsonIn := fmt.Sprintf( `{
        "duration": 1,
        "pressure": %d
    }`, pressure )
    
    self.sessionCall( "/wda/element/" + elId + "/forceTouch", jsonIn )
}

func (self *WDA) ElLongTouch( elId string ) {
    jsonIn := fmt.Sprintf( `{
        "duration": 2
    }` )
    
    self.sessionCall( "/wda/element/" + elId + "/touchAndHold", jsonIn )
}

func (self *WDA) ElByName( elName string ) string {
    fmt.Printf("Finding element named %s\n", elName )
    jsonIn := fmt.Sprintf( `{
        "using": "name",
        "value": "%s"
    }`, elName )
    
    resp := self.sessionCall( "/element", jsonIn )
        
    for i:=0; i<5; i++ {
        if resp != nil {
            break
        }
        
        fmt.Printf("null response attempting to find element named %s\n", elName )
        time.Sleep( time.Second * 1 )
        resp = self.sessionCall( "/element", jsonIn )
    }
    
    elNode := resp.Get("ELEMENT")
    if elNode == nil { return "" }
    return elNode.String()
}

func (self *WDA) WindowSize() (int,int) {
    resp, _ := http.Get( self.base + "/session/" + self.sessionId + "/window/size" )
    
    json := resp_to_str( resp )
    
    root, _, parseErr := uj.ParseFull( []byte(json) )
    if parseErr != nil {
       fmt.Printf("Parse error: %v\n", parseErr )
       fmt.Println( json )
       panic("/window/size WDA call did not return valid JSON")
    }
    rootVal := root.Get("value")
    if rootVal == nil {
        fmt.Println( json )
        panic("/window/size WDA call didn't return a 'value'")
    }
    
    wid := rootVal.Get("width").Int()
    heg := rootVal.Get("height").Int()
    
    return wid,heg
}

func (self *WDA) Source() string {
    resp, _ := http.Get( self.base + "/source" )
    
    val, _ := resp_to_val( resp )
    
    xmlSource := val.String()
    
    xmlSource = strings.ReplaceAll( xmlSource, "\\n", "\n" )
    
    return xmlSource
}

/*
This function is actually no longer used by the system as broadcast is started via the CFStream
app itself rather than through the Control Center now.

Leaving it here in case it is needed in the future for something.
*/
func (self *WDA) OpenControlCenter( controlCenterMethod string ) {
    fmt.Printf("Opening control center\n")  
    width, height := self.WindowSize()
    
    if controlCenterMethod == "bottomUp" {
        midx := width / 2
        maxy := height - 1
        self.swipe( midx, maxy, midx, maxy - 100 )
    } else if controlCenterMethod == "topDown" {
        maxx := width - 1
        self.swipe( maxx, 0, maxx, 100 )
    }    
}

func (self *WDA) StartBroadcastStream( appName string, bid string ) {
    sid := self.create_session( bid )
    self.sessionId = sid
    
    toSelector := self.ElByName( "  Broadcast Selector" )
    self.ElClick( toSelector )
    
    time.Sleep( time.Second * 4 )
    self.Source()
    
    startBtn := self.ElByName( "Start Broadcast" )
    self.ElClick( startBtn )
    
    time.Sleep( time.Second * 5 )
}

func (self *WDA) AppChanged() {
    self.sessionCallGet( "/updateApplication" )
}