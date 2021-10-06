package main

import (
    "bytes"
    "bufio"
    "fmt"
    //"io/ioutil"
    "image/png"
    "image/jpeg"
    uj "github.com/nanoscopic/ujsonin/v2/mod"
    log "github.com/sirupsen/logrus"
    nr "github.com/nfnt/resize"
    "net/http"
    "os"
    "os/exec"
    "strings"
    "strconv"
    "time"
    //"go.nanomsg.org/mangos/v3"
    //nanoReq  "go.nanomsg.org/mangos/v3/protocol/req"
)

type IIFBridge struct {
  onConnect    func( dev BridgeDev ) ProcTracker
  onDisconnect func( dev BridgeDev )
  cli          string
  devs         map[string]*IIFDev
  procTracker  ProcTracker
  config       *Config
}

type IIFDev struct {
  bridge      *IIFBridge
  udid        string
  name        string
  procTracker ProcTracker
  config      *CDevice
  device      *Device
}

// IosIF bridge
func NewIIFBridge( config *Config, OnConnect func( dev BridgeDev ) (ProcTracker), OnDisconnect func( dev BridgeDev ), iosIfPath string, procTracker ProcTracker, detect bool ) ( BridgeRoot ) {
  self := &IIFBridge{
    onConnect:    OnConnect,
    onDisconnect: OnDisconnect,
    cli:          iosIfPath,
    devs:         make( map[string]*IIFDev ),
    procTracker:  procTracker,
    config:       config,
  }
  if detect { self.startDetect() }
  return self
}

func (self *IIFDev) getUdid() string {
  return self.udid
}

func (self *IIFBridge) startDetect() {
  o := ProcOptions{
    procName: "device_trigger",
    binary: self.cli,
    args: []string{ "detectloop" },
    stdoutHandler: func( line string, plog *log.Entry ) {
    },
    stderrHandler: func( line string, plog *log.Entry ) {
      if strings.HasPrefix( line, "{" ) {
        root, _ := uj.Parse( []byte(line) )
        evType := root.Get("type").String()
        udid := root.Get("udid").String()
        if evType == "connect" {
          name := root.Get("name").String()
          self.OnConnect( udid, name, plog )
        } else if evType == "disconnect" {
          self.OnDisconnect( udid, plog )
        }
      }
    },
    onStop: func( interface{} ) {
      log.Println("device trigger stopped")
    },
  }
  proc_generic( self.procTracker, nil, &o )
}

func (self *IIFBridge) list() []BridgeDevInfo {
  infos := []BridgeDevInfo{}
  for _,dev := range self.devs {
    infos = append( infos, BridgeDevInfo{ udid: dev.udid } )
  }
  return infos
}

func (self *IIFBridge) OnConnect( udid string, name string, plog *log.Entry ) {
  dev := NewIIFDev( self, udid, name, nil )
  self.devs[ udid ] = dev
  
  devConfig, hasDevConfig := self.config.devs[ udid ]
  if hasDevConfig {
    dev.config = &devConfig
  }
  
  dev.procTracker = self.onConnect( dev )
}

func (self *IIFBridge) OnDisconnect( udid string, plog *log.Entry ) {
  dev := self.devs[ udid ]
  dev.destroy()
  self.onDisconnect( dev )
  delete( self.devs, udid )
}

func (self *IIFBridge) destroy() {
  for _,dev := range self.devs {
    dev.destroy()
  }
  // close self processes
}

func NewIIFDev( bridge *IIFBridge, udid string, name string, device *Device ) (*IIFDev) {
  log.WithFields( log.Fields{
      "type": "iifdev_create",
      "udid": censorUuid( udid ),
  } ).Debug( "Creating IIFDev" )
  
  var procTracker ProcTracker = nil
  return &IIFDev{
    bridge: bridge,
    name: name,
    udid: udid,
    procTracker: procTracker,
    device: device,
  }
}

func (self *IIFDev) setProcTracker( procTracker ProcTracker ) {
  self.procTracker = procTracker
}

//func (self *IIFDev) tunnel( pairs []TunPair, onready func() ) {
//  self.tunnelIosif( pairs, onready )
//}

func (self *IIFDev) tunnelIproxy( pairs []TunPair, onready func() ) {
  tunName := "tunnel"
  specs := []string{}
  for _,pair := range pairs {
    tunName = fmt.Sprintf( "%s_%d->%d", tunName, pair.from, pair.to )
    specs = append( specs, fmt.Sprintf("%d:%d",pair.from,pair.to) )
  }
  
  args := []string {
    "-u", self.udid,
  }
  args = append( args, specs... )
  fmt.Printf("Starting %s with %s\n", "/usr/local/bin/iproxy", args )
  
  o := ProcOptions{
    procName: tunName,
    binary: "/usr/local/bin/iproxy",
    args: args,
    stdoutHandler: func( line string, plog *log.Entry ) {
      fmt.Printf( "tunnel:%s\n", line )
      if strings.Contains( line, "waiting" ) {
        if onready != nil {
          //onready()
        }
      }
    },
    stderrHandler: func( line string, plog *log.Entry ) {
      fmt.Printf( "tunnel err:%s\n", line )
      
    },
    onStop: func( interface{} ) {
      log.Printf("%s stopped\n", tunName)
    },
  }
  proc_generic( self.procTracker, nil, &o )
  time.Sleep( time.Second * 2 )
  onready()
}

func (self *IIFDev) tunnelGoIos( pairs []TunPair, onready func() ) {
  count := len( pairs )
  sofar := 0
  done := make( chan bool )
  for _,pair := range( pairs ) {
    self.tunnelOne( pair, func() {
      sofar++
      if sofar == count {
        done <- true
      }
    } )
  }
  <- done
  onready()
}

func (self *IIFDev) tunnelOne( pair TunPair, onready func() ) {
  tunName := "tunnel"
  specs := []string{}
  
  tunName = fmt.Sprintf( "%s_%d->%d", tunName, pair.from, pair.to )
  specs = append( specs, fmt.Sprintf("%d",pair.from) )
  specs = append( specs, fmt.Sprintf("%d",pair.to) )
  
  args := []string {
    "forward",
    "--udid", self.udid,
  }
  args = append( args, specs... )
  fmt.Printf("Starting %s with %s\n", self.bridge.cli, args )
  
  o := ProcOptions{
    procName: tunName,
    binary: "bin/go-ios",
    args: args,
    stdoutHandler: func( line string, plog *log.Entry ) {
      fmt.Println( "tunnel:%s", line )
    },
    stderrHandler: func( line string, plog *log.Entry ) {
      
      //fmt.Println( "tunnel:%s", line )
      if strings.Contains( line, "Start" ) {
        if onready != nil {
          onready()
        }
        fmt.Printf( "tunnel start:%s\n", line )
      } else {
        fmt.Printf( "tunnel err:%s\n", line )
      }
    },
    onStop: func( interface{} ) {
      log.Printf("%s stopped\n", tunName)
    },
  }
  proc_generic( self.procTracker, nil, &o )
}

func (self *IIFDev) tunnel( pairs []TunPair, onready func() ) {
  tunName := "tunnel"
  specs := []string{}
  for _,pair := range pairs {
    from := pair.from
    to := pair.to
    
    tunName = fmt.Sprintf( "%s_%d->%d", tunName, from, to )
    //specs = append( specs, fmt.Sprintf("%d:%d",from,to) )
    specs = append( specs, strconv.Itoa( from ) + ":" + strconv.Itoa( to ) )
  }
  
  args := []string {
    "tunnel",
    "-id", self.udid,
  }
  args = append( args, specs... )
  fmt.Printf("Starting %s with %s\n", self.bridge.cli, args )
  
  o := ProcOptions{
    procName: tunName,
    binary: self.bridge.cli,
    args: args,
    stdoutHandler: func( line string, plog *log.Entry ) {
      //fmt.Printf( "tunnel:%s\n", line )
      if strings.Contains( line, "Ready" ) {
        if onready != nil {
          onready()
        }
      }
    },
    stderrHandler: func( line string, plog *log.Entry ) {
      //fmt.Printf( "tunnel err:%s\n", line )
    },
    onStop: func( interface{} ) {
      log.Printf("%s stopped\n", tunName)
    },
  }
  proc_generic( self.procTracker, nil, &o )
}

func (self *IIFBridge) GetDevs( config *Config ) []string {
  json, _ := exec.Command( config.iosIfPath,
    []string{ "list", "-json" }... ).Output()
  root, _ := uj.Parse( []byte( "[" + string(json) + "]" ) )
  res := []string{}
  root.ForEach( func( dev uj.JNode ) {
      res = append( res, dev.Get("udid").String() )
  } )
  return res
}

func (self *IIFDev) GetPid( appname string ) uint64 {
  json, err := exec.Command( self.bridge.cli,
    []string{
      "ps",
      "-raw",
      "-appname", appname,
    }... ).Output()
    
  if err != nil {
    return 0
  }
  
  jsonS := string( json )
  jsonS = strings.ReplaceAll( jsonS, "i16.", "" )
  jsonS = strings.ReplaceAll( jsonS, "i32.", "" )
  root, _ := uj.Parse( []byte( jsonS ) )
  pidNode := root.Get("pid")
  if pidNode == nil { return 0 }
  return uint64( pidNode.Int() )
}

func (self *IIFDev) Kill( pid uint64 ) {
}

func (self *IIFDev) AppInfo( bundleId string ) uj.JNode {
  json, err := exec.Command( self.bridge.cli,
    []string{
      "listapps",
      "-bi", bundleId,
    }... ).Output()
  
  if err != nil { return nil }
  
  root, _ := uj.Parse( json )
  return root
}

func (self *IIFDev) InstallApp( appPath string ) bool {
  status, _ := exec.Command( self.bridge.cli,
    []string{
      "install",
      "-path", appPath,
    }... ).Output()
  
  if strings.Contains( string(status), "Installing:100%" ) {
    return true
  }
  return false      
}

func (self *IIFDev) info( names []string ) map[string]string {
  mapped := make( map[string]string )
  //fmt.Printf("udid for info: %s\n", self.udid )
  args := []string {
    "info",
    "-json",
    "-id", self.udid,
  }
  args = append( args, names... )
  json, _ := exec.Command( self.bridge.cli, args... ).Output()
  //fmt.Printf("json:%s\n",json)
  root, _ := uj.Parse( json )
  
  for _,name := range names {
    node := root.Get(name)
    if node != nil {
      mapped[name] = node.String()
    }
  }
  //fmt.Printf("mapped result:%s\n",mapped)
  
  return mapped
}

func (self *IIFDev) gestalt( names []string ) map[string]string {
  mapped := make( map[string]string )
  args := []string{
    "mg",
    "-json",
    "-id", self.udid,
  }
  args = append( args, names... )
  fmt.Printf("Running %s %s\n", self.bridge.cli, args );
  json, _ := exec.Command( self.bridge.cli, args... ).Output()
  fmt.Printf("json:%s\n",json)
  root, _ := uj.Parse( json )
  for _,name := range names {
    node := root.Get(name)
    if node != nil {
      mapped[name] = node.String()
    }
  }
  
  return mapped
}

func (self *IIFDev) gestaltnode( names []string ) map[string]uj.JNode {
  mapped := make( map[string]uj.JNode )
  args := []string{
    "mg",
    "-json",
    "-id", self.udid,
  }
  args = append( args, names... )
  fmt.Printf("Running %s %s\n", self.bridge.cli, args );
  json, _ := exec.Command( self.bridge.cli, args... ).Output()
  fmt.Printf("json:%s\n",json)
  root, _ := uj.Parse( json )
  for _,name := range names {
    node := root.Get(name)
    if node != nil {
      mapped[name] = node
    }
  }
  
  return mapped
}

func (self *IIFDev) ps() []iProc {
    return []iProc{}
}

func (self *IIFDev) screenshot() Screenshot {
    return Screenshot{}
}

type BackupVideoIIF struct {
    port int
    //sock mangos.Socket
    spec string
    imgId int
}

func (self *IIFDev) NewSyslogMonitor( handleLogItem func( msg string, app string ) ) {
    bufstr := ""
    toFetch := 0
    o := ProcOptions{
        procName: "syslogMonitor",
        binary: self.bridge.cli,
        args: []string {
            "log",
            "-id", self.udid,
            "proc", "SpringBoard(SpringBoard)",
            "proc", "SpringBoard(FrontBoard)",
            "proc", "dasd",
        },
        startFields: log.Fields{
            "id": self.udid,
        },
        stdoutHandler: func( line string, plog *log.Entry ) {
            if line[0] == '*' {
                i:=1
                for ;i<6;i++ {
                    char := line[i]
                    if char == '[' {
                        break
                    }
                }
                bytesStr := line[ 1: i ]
                toFetch, _ = strconv.Atoi( bytesStr )
                toFetch--
                
                rest := line[ i: ]
                //fmt.Printf("msg len: %d -- want: %d\n", len(rest), toFetch )
                if len( rest ) == toFetch {
                    json := line[ i: ]
                    root, _, err := uj.ParseFull( []byte( json ) )
                    if err == nil {
                        msg := root.GetAt( 3 ).String()
                        app := root.GetAt( 1 ).String()
                        handleLogItem( msg, app )
                    } else {
                        fmt.Printf("Could not parse:[%s]\n", json )
                    }
                } else {
                    bufstr = rest
                    toFetch -= len( rest )
                }
            } else if toFetch > 0 {
                if len( line ) < toFetch {
                    toFetch -= len( line )
                    bufstr = bufstr + line
                } else if len( line ) >= toFetch {
                    bufstr = bufstr + line
                    
                    root, _, err := uj.ParseFull( []byte( bufstr ) )
                    if err == nil {
                        msg := root.GetAt( 3 ).String()
                        app := root.GetAt( 1 ).String()
                        handleLogItem( msg, app )
                    } else {
                        fmt.Printf("Could not parse:[%s]\n", bufstr )
                    }
                }
                
            }
        },
    }
    
    proc_generic( self.procTracker, nil, &o )
}

func (self *IIFDev) NewBackupVideo( port int, onStop func( interface{} ) ) BackupVideo {
    vid := &BackupVideoIIF{
        port: port,
        spec: fmt.Sprintf( "http://127.0.0.1:%d", port ),
    }
    
    o := ProcOptions{
        procName: "backupVideo",
        binary: self.bridge.cli,
        args: []string {
            "iserver",
            "-port", strconv.Itoa( port ),
            "-id", self.udid,
        },
        startFields: log.Fields{
            "port": strconv.Itoa( port ),
            "id": self.udid,
        },
        onStop: func( wrapper interface{} ) {
            onStop( wrapper )
        },
        stdoutHandler: func( line string, plog *log.Entry ) {
            if strings.Contains( line, "listening" ) {
                plog.Println( line )
                //vid.openBackupStream()
            }
            
            //fmt.Printf( "backup video:%s\n", line )
        },
        stderrHandler: func( line string, plog *log.Entry ) {
            //fmt.Printf( "backup video err:%s\n", line )
        },
    }
        
    proc_generic( self.procTracker, nil, &o )
    
    return vid
}

func (self *BackupVideoIIF) GetFrame() []byte {
    resp, err := http.Get( self.spec )
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()
    //data, _ := ioutil.ReadAll( resp.Body )
    
    data := resp.Body
    img, err := png.Decode( data )
    if err != nil {
        fmt.Printf("Could not decode backup video frame: %s\n", err )
        //if length( data ) < 300 {
        //    fmt.Printf("Data: %s\n", data )
        //}
        return []byte{}
    }
    img2 := nr.Resize( 0, 1000, img, nr.Lanczos3 )
    
    jpegBytes := bytes.Buffer{}
    writer := bufio.NewWriter( &jpegBytes )
    jpeg.Encode( writer, img2, nil )
    
    return jpegBytes.Bytes()
}

func (self *IIFDev) cfa( onStart func(), onStop func(interface{}) ) {
    config := self.bridge.config
    method := config.cfaMethod
    
    if method == "go-ios" {
        self.cfaGoIos( onStart, onStop )
    } else if method == "tidevice" {
        self.cfaTidevice( onStart, onStop )
    } else if method == "manual" {
        //self.wdaTidevice( port, onStart, onStop, mjpegPort )
    } else {
        fmt.Printf("Unknown cfa start method %s\n", method )
        os.Exit(1)
    }
}

func (self *IIFDev) cfaGoIos( onStart func(), onStop func(interface{}) ) {
    f, err := os.OpenFile("cfa.log",
        os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        log.WithFields( log.Fields{
            "type": "wda_log_fail",
        } ).Fatal("Could not open cfa.log for writing")
    }
    
    config := self.bridge.config
    biPrefix := config.wdaPrefix
    bi := fmt.Sprintf( "%s.CFAgentRunner.xctrunner", biPrefix )
    
    args := []string{
        "runwda",
        "--bundleid", bi,
        "--testrunnerbundleid", bi,
        "--xctestconfig", "CFAgentRunner.xctest",
        "--udid", self.udid,
    }
    
    fmt.Fprintf( f, "Starting CFA via %s with args %s\n", "bin/go-ios", strings.Join( args, " " ) )
    fmt.Printf( "Starting CFA via %s with args %s\n", "bin/go-ios", strings.Join( args, " " ) )
    
    o := ProcOptions {
        procName: "cfa",
        binary: "bin/go-ios",
        args: args,
        stdoutHandler: func( line string, plog *log.Entry ) {
            if strings.Contains( line, "configuration is unsupported" ) {
                plog.Println( line )
            }
            fmt.Fprintf( f, "runcfa: %s\n", line )
        },
        stderrHandler: func( line string, plog *log.Entry ) {
            if strings.Contains(line, "NNG Ready") {
                plog.WithFields( log.Fields{
                    "type": "cfa_start",
                    "uuid": censorUuid(self.udid),
                } ).Info("[CFA] successfully started")
                onStart()
            }
            if strings.Contains( line, "configuration is unsupported" ) {
                plog.Println( line )
            }
            fmt.Fprintf( f, "runcfa: %s\n", line )
        },
        onStop: func( wrapper interface{} ) {
            onStop( wrapper )
        },
    }
    
    proc_generic( self.procTracker, nil, &o )
}

func (self *IIFDev) cfaTidevice( onStart func(), onStop func(interface{}) ) {
    config := self.bridge.config
    tiPath := config.tidevicePath
    
    if tiPath == "" {
        log.WithFields( log.Fields{
            "type":  "tidevice_path_unset",
        } ).Fatal("tidevice path is unknown. Run `make usetidevice` to correct")
    }
    
    f, err := os.OpenFile("cfa.log",
        os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        log.WithFields( log.Fields{
            "type": "cfa_log_fail",
        } ).Fatal("Could not open cfa.log for writing")
    }
    
    biPrefix := config.cfaPrefix
    bi := fmt.Sprintf( "%s.CFAgentRunner.xctrunner", biPrefix )
    
    args := []string{
        "-u", self.udid,
        "wdaproxy",
        "-B", bi,
        "-p", "0",
    }
    
    fmt.Fprintf( f, "Starting CFA via %s with args %s\n", tiPath, strings.Join( args, " " ) )
    
    o := ProcOptions {
        procName: "wda",
        binary: tiPath,
        args: args,
        stderrHandler: func( line string, plog *log.Entry ) {
            if strings.Contains(line, "CFAgent start successfully") {
                plog.WithFields( log.Fields{
                    "type": "cfa_start",
                    "uuid": censorUuid(self.udid),
                } ).Info("[CFA] successfully started")
                onStart()
            }
            if strings.Contains( line, "have to mount the Developer disk image" ) {
                plog.WithFields( log.Fields{
                    "type": "cfa_start_err",
                    "uuid": censorUuid(self.udid),
                } ).Fatal("[CFA] Developer disk not mounted. Cannot start CFA")
            }
            if strings.Contains( line, "'No app matches'" ) {
                plog.WithFields( log.Fields{
                    "type": "cfa_start_err",
                    "uuid": censorUuid(self.udid),
                    "rawErr": line,
                } ).Fatal("[CFA] Incorrect CFA bundle id")
            }
            fmt.Fprintln( f, line )
        },
        onStop: func( wrapper interface{} ) {
            onStop( wrapper )
        },
    }
    
    proc_generic( self.procTracker, nil, &o )
}

func (self *IIFDev) destroy() {
  // close running processes
}

func (self *IIFDev) SetConfig( config *CDevice ) {
    self.config = config
}

func (self *IIFDev) SetDevice( device *Device ) {
    self.device = device
}