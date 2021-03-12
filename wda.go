package main

import (
    //"bytes"
    "fmt"
    "io/ioutil"
    "net/http"
    "os"
    //"os/exec"
    "strings"
    "path/filepath"
    "regexp"
    "strconv"
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
    
    xctestrunFile := findXctestrun("./bin/wda")
    if xctestrunFile == "" {
        log.Fatal("Could not find xctestrun of sufficient version")
        return
    }
    
    self.dev.bridge.wda(
        xctestrunFile,
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

func findXctestrun(folder string) string {
    iosversion := ""
    
    if _, err := os.Stat( folder ); os.IsNotExist( err ) {
        log.Warn( fmt.Sprintf( "Directory %s does not exist; WDA not built? Run `make wda`\n", folder ) )
        return ""
    }
    
    folder, _ = filepath.EvalSymlinks( folder )
    
    var files []string
    err := filepath.Walk(folder, func( file string, info os.FileInfo, err error ) error {
        if err != nil { return nil }
        if info.IsDir() && folder != file {
            //fmt.Printf("skipping %s\n", file)
            return filepath.SkipDir
        }
        files = append( files, file )
        return nil
    } )
    if err != nil {
        log.Fatal(err)
    }
    
    versionMatch := false
    var findMajor int64 = 0
    var findMinor int64 = 0
    var curMajor int64 = 100
    var curMinor int64 = 100
    if iosversion != "" {
        parts := strings.Split( iosversion, "." )
        findMajor, _ = strconv.ParseInt( parts[0], 10, 64 )
        findMinor, _ = strconv.ParseInt( parts[1], 10, 64 )
        versionMatch = true
    }
    
    xcFile := ""
    for _, file := range files {
        fmt.Printf("Found file %s\n", file )
        if ! strings.HasSuffix(file, ".xctestrun") {
            continue
        }
        
        if ! versionMatch {
            xcFile = file
            break
        }
        
        r := regexp.MustCompile( `iphoneos([0-9]+)\.([0-9]+)` )
        fileParts := r.FindSubmatch( []byte( file ) )
        fileMajor, _ := strconv.ParseInt( string(fileParts[1]), 10, 64 )
        fileMinor, _ := strconv.ParseInt( string(fileParts[2]), 10, 64 )
        
        // Find the smallest file version greater than or equal to the ios version
        // Golang line continuation for long boolean expressions is horrible. :(
        
        // Checked file version smaller than current file version
        // &&
        // Checked file version greater or equal to ios version    
        if ( fileMajor < curMajor  || ( fileMajor == curMajor  && fileMinor <= curMinor  ) ) &&
           ( fileMajor > findMajor || ( fileMajor == findMajor && fileMinor >= findMinor ) ) {
              curMajor = fileMajor
              curMinor = fileMinor
              xcFile = file
        }
    }
    return xcFile
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
  if !strings.HasPrefix( rawContent, "{" ) {
    return nil // &JHash{ nodeType: 1, hash: NewNodeHash() }
  }
  content, _ := uj.Parse( []byte( rawContent ) )
  val := content.Get("value")
  if val == nil { return content }
  return val
}

func ( self *WDA ) create_session( bundle string ) ( string ) {
  time.Sleep( time.Second * 3 )
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

func (self *WDA) clickAt( x int, y int ) (string) {
    json := fmt.Sprintf( `{
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
    resp, _ := http.Post( self.base + "/session/" + self.sessionId + "/wda/touch/perform", "application/json", strings.NewReader( json ) )
    res := resp_to_str( resp )
    log.Info( "response " + res )
    return res    
}

func (self *WDA) hardPress( x int, y int ) (string) {
  log.Info( "Hard Press:", x, y )
    json := fmt.Sprintf( `{
        "actions":[
            {
              "action": "press",
              "options": {
                "x":%d,
                "y":%d,
                "pressure":2000
              }
            },
            {
              "action":"wait",
              "options": {
                "ms": 100
              }
            },
            {
              "action":"release",
              "options":{}
            }
        ]
    }`, x, y )
    resp, _ := http.Post( self.base + "/session/" + self.sessionId + "/wda/touch/perform", "application/json", strings.NewReader( json ) )
    res := resp_to_str( resp )
    log.Info( "response " + res )
    return res    
}

func (self *WDA) longPress( x int, y int ) (string) {
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
    resp, _ := http.Post( self.base + "/session/" + self.sessionId + "/wda/touch/perform", "application/json", strings.NewReader( json ) )
    res := resp_to_str( resp )
    log.Info( "response " + res )
    return res
}

func (self *WDA) home() (string) {
    resp, _ := http.Post( self.base + "/wda/homescreen", "application/json", strings.NewReader( "{}" ) )
    res := resp_to_str( resp )
    log.Info( "response " + res )
    return res    
}

func ( self *WDA ) swipe( x1 int, y1 int, x2 int, y2 int ) ( string ) {
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
    resp, _ := http.Post( self.base + "/session/" + self.sessionId + "/wda/touch/perform", "application/json", strings.NewReader( json ) )
    res := resp_to_str( resp )
    log.Info( "response " + res )
    return res
}

func (self *WDA) ElClick( elId string ) {
    http.Post(
        self.base + "/session/" + self.sessionId + "/element/" + elId + "/click",
        "application/json",
        strings.NewReader( "{}" ),
        )
}

func (self *WDA) ElForceTouch( elId string, pressure int ) {
    jsonIn := fmt.Sprintf( `{
        "duration": 1,
        "pressure": %d
    }`, pressure )
    
    resp, _ := http.Post(
        self.base + "/session/" + self.sessionId + "/wda/element/" + elId + "/forceTouch",
        "application/json",
        strings.NewReader( jsonIn ),
        )
    
    json := resp_to_str( resp )
    fmt.Println( json )
}

func (self *WDA) ElByName( elName string ) string {
    jsonIn := fmt.Sprintf( `{
        "using": "name",
        "value": "%s"
    }`, elName )
    
    resp, _ := http.Post(
        self.base + "/session/" + self.sessionId + "/element", "application/json",
        strings.NewReader( jsonIn ),
        )
    
    json := resp_to_str( resp )
    //fmt.Println( json )
    
    root, _ := uj.Parse( []byte(json) )
    elNode := root.Get("value").Get("ELEMENT")
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

func (self *WDA) OpenControlCenter() {
    width, height := self.WindowSize()
    
    midx := width / 2
    maxy := height - 1
    self.swipe( midx, maxy, midx, maxy - 100 )
}

func (self *WDA) StartBroadcastStream( appName string ) {
  self.OpenControlCenter()
  
  devEl := self.ElByName( "Screen Recording" )
  
  self.ElForceTouch( devEl, 2000 )
  
  appEl := self.ElByName( appName )
  self.ElClick( appEl )
  
  startBtn := self.ElByName( "Start Broadcast" )
  self.ElClick( startBtn )
  
  time.Sleep( time.Second * 5 )
}