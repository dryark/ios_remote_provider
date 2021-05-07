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
    udid         string
    onDevicePort int
    localhostPort int
    devTracker   *DeviceTracker
    dev          *Device
    wdaProc      *GenericProc
    config       *Config
    base         string
    sessionId    string
    startChan    chan bool
}

func NewWDA( config *Config, devTracker *DeviceTracker, dev *Device, localhostPort int ) (*WDA) {
    self := WDA{
        udid: dev.udid,
        onDevicePort: 8100,
        localhostPort: localhostPort,
        devTracker: devTracker,
        dev: dev,
        config: config,
        base: fmt.Sprintf("http://127.0.0.1:%d",localhostPort),
    }
    
    self.start()
    
    return &self
}

func (self *WDA) start() {
    pairs := []TunPair{
        TunPair{ from: self.localhostPort, to: self.onDevicePort },
    }
    self.dev.bridge.tunnel( pairs )
    
    self.dev.bridge.wda(
        self.localhostPort,
        func() { // onStart
            log.WithFields( log.Fields{
                "type": "wda_start",
                "udid":  censorUuid(self.udid),
                "port": self.localhostPort,
            } ).Info("[WDA] successfully started")
            if self.startChan != nil {
                self.startChan <- true
            }
            self.dev.EventCh <- DevEvent{
                action: 1,
            }
        },
        func(interface{}) { // onStop
            self.dev.EventCh <- DevEvent{
                action: 2,
            }
        },
    )
}

func (self *WDA) stop() {
    if self.wdaProc != nil {
        self.wdaProc.Kill()
        self.wdaProc = nil
    }
}

func (self *WDA) ensureSession() {
    sid := self.create_session( "" )
    fmt.Printf("Created wda session id=%s\n", sid )
    self.sessionId = sid
}

func resp_to_str( resp *http.Response ) ( string ) {
    data, _ := ioutil.ReadAll( resp.Body )
    resp.Body.Close()
    return string(data)
    //body := resp.Body
    //buf := new( bytes.Buffer )
    //buf.ReadFrom( body )
    //return buf.String()  
}

func resp_to_val( resp *http.Response ) ( uj.JNode ) {
  rawContent := resp_to_str( resp )
  if len( rawContent ) == 0 { return nil }
  if !strings.HasPrefix( rawContent, "{" ) {
    return nil // &JHash{ nodeType: 1, hash: NewNodeHash() }
  }
  content, _ := uj.Parse( []byte( rawContent ) )
  val := content.Get("value")
  if val == nil { return content }
  return val
}

func ( self *WDA ) create_session( bundle string ) ( string ) {
  time.Sleep( time.Second * 4 )
  ops := fmt.Sprintf( `{
    "capabilities": {
      "alwaysMatch": {},
      "firstMatch": [
        {
          
        }
      ]
    }
  }` );
  resp, err := http.Post( self.base + "/session", "application/json; charset=UTF-8", strings.NewReader( ops ) )
  if err != nil {
      panic( err )
  }
  if resp.StatusCode != 200 {
      str := resp_to_str( resp )
      
      fmt.Printf("Got status %d back from query to %s\nstr = %s\n", resp.StatusCode, self.base + "/session", str )
      return ""
  }
  
  res := resp_to_val( resp )
  //fmt.Printf("result from create session: %s\n", res )
  return res.Get("sessionId").String()
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
    
    val = resp_to_val( resp )
    val.Dump()
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
            val = resp_to_val( resp )
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
    strArr := []string{}
    for _, code := range codes {
        if code >= 97 && code <= 122 {
            strArr = append( strArr, fmt.Sprintf("\"%c\"", rune( code ) ) )
        } else {
            strArr = append( strArr, fmt.Sprintf("\"\\u%04x\"", code ) )
        }
    }
    
    json := fmt.Sprintf(`{
        "value": [%s]
    }`, strings.Join( strArr, "," ) )
    
    log.Info( "sending " + json )
    
    self.sessionCall( "/wda/keys", json )
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

func (self *WDA) ElByName( elName string ) string {
    jsonIn := fmt.Sprintf( `{
        "using": "name",
        "value": "%s"
    }`, elName )
    
    resp := self.sessionCall( "/element", jsonIn )
        
    //fmt.Println( json )
    
    for i:=0; i<5; i++ {
        if resp != nil {
            break
        }
        
        fmt.Printf("null response attempting to find element named %s\n", elName )
        time.Sleep( time.Second * 1 )
        resp = self.sessionCall( "/element", jsonIn )
        //source := self.Source()
        //fmt.Printf("page source:%s\n", source )
        //panic("err")
        //}
    }
    
    elNode := resp.Get("ELEMENT")
    if elNode == nil { return "" }
    return elNode.String()
}

func (self *WDA) WindowSize() (int,int) {
    resp, _ := http.Get( self.base + "/session/" + self.sessionId + "/window/size" )
    
    json := resp_to_str( resp )
    //fmt.Println( json )
    
    root, _ := uj.Parse( []byte(json) )
    wid := root.Get("value").Get("width").Int()
    heg := root.Get("value").Get("height").Int()
    
    return wid,heg
}

func (self *WDA) Source() string {
    resp, _ := http.Get( self.base + "/source" )
    
    val := resp_to_val( resp )
    
    xmlSource := val.String()
    
    xmlSource = strings.ReplaceAll( xmlSource, "\\n", "\n" )
    
    return xmlSource
}

func (self *WDA) OpenControlCenter() {
    width, height := self.WindowSize()
    
    midx := width / 2
    maxy := height - 1
    self.swipe( midx, maxy, midx, maxy - 100 )
}

func (self *WDA) StartBroadcastStream( appName string ) {
  self.OpenControlCenter()
  time.Sleep( time.Second * 2 )
  
  devEl := self.ElByName( "Screen Recording" )
  
  self.ElForceTouch( devEl, 2000 )
  
  time.Sleep( time.Second * 2 )
  
  appEl := self.ElByName( appName )
  self.ElClick( appEl )
  
  startBtn := self.ElByName( "Start Broadcast" )
  self.ElClick( startBtn )
  
  time.Sleep( time.Second * 5 )
}