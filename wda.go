package main

import (
    //"bytes"
    "fmt"
    "io/ioutil"
    "net/http"
    "os"
    "os/exec"
    "strings"
    "path/filepath"
    "regexp"
    "strconv"
    "time"
    log "github.com/sirupsen/logrus"
    uj "github.com/nanoscopic/ujsonin/mod"
)

type WDA struct {
    uuid         string
    onDevicePort int
    localhostPort int
    devTracker   *DeviceTracker
    dev          *Device
    wdaProc      *GenericProc
    config       *Config
    base         string
    sessionId    string
}

func NewWDA( config *Config, devTracker *DeviceTracker, dev *Device, localhostPort int ) (*WDA) {
    self := WDA{
        uuid: dev.uuid,
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
    go func() {
        spec := fmt.Sprintf("%d:%d", self.localhostPort, self.onDevicePort )
        
        log.WithFields( log.Fields{
            "bin": self.config.mobiledevicePath,
            "uuid": censorUuid( self.uuid ),
            "spec": spec,
        } ).Info("Process start tunnel")
        
        c := exec.Command( self.config.mobiledevicePath, "tunnel", "-u", self.uuid, spec )
        
        c.Stdout = os.Stdout
        c.Stderr = os.Stderr
        
        /*err := */c.Run()
        fmt.Printf("mobileDevice tunnel failure\n")
    }()
    
    xctestrunFile := findXctestrun("./bin/wda")
    if xctestrunFile == "" {
        log.Fatal("Could not find WebDriverAgent.xcodeproj or xctestrun of sufficient version")
        return
    }
    o := ProcOptions {
        procName: "wda",
        binary: "xcodebuild",
        startDir: "./bin/wda",
        args: []string{
            "test-without-building",
            "-xctestrun", xctestrunFile,
            "-destination", "id="+self.uuid,
        },
        startFields: log.Fields{
            "testrun": xctestrunFile,
        },
        stdoutHandler: func( line string, plog *log.Entry ) {
            //if debug {
            //    fmt.Printf("[WDA] %s\n", lineStr)
            //}
            if strings.HasPrefix(line, "Test Case '-[UITestingUITests testRunner]' started") {
                plog.Println("[WDA] successfully started")
                self.dev.EventCh <- DevEvent{
                    action: 1,
                }
            }
            if strings.Contains( line, "configuration is unsupported" ) {
                plog.Println( line )
            }
            //plog.Println( line )
        },
        stderrHandler: func( line string, plog *log.Entry ) {
            if strings.Contains( line, "configuration is unsupported" ) {
                plog.Println( line )
            }
            //plog.Println( line )
        },
        onStop: func( *Device ) {
            self.dev.EventCh <- DevEvent{
                action: 2,
            }
        },
    }
    
    self.wdaProc = proc_generic( self.devTracker, self.dev, &o )
}

func (self *WDA) stop() {
    if self.wdaProc != nil {
        self.wdaProc.Kill()
        self.wdaProc = nil
    }
}

func findXctestrun(folder string) string {
    iosversion := ""
    
    folder, _ = filepath.EvalSymlinks( folder )
    
    var files []string
    err := filepath.Walk(folder, func( file string, info os.FileInfo, err error ) error {
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

func resp_to_val( resp *http.Response ) ( *uj.JNode ) {
  rawContent := resp_to_str( resp )
  if !strings.HasPrefix( rawContent, "{" ) {
    return nil // &JNode{ nodeType: 1, hash: NewNodeHash() }
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

func ( self *WDA ) swipe( sid string, x1 int, y1 int, x2 int, y2 int ) ( string ) {
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
