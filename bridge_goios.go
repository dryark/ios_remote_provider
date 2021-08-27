package main

import (
  "fmt"
  "os"
  "os/exec"
  "strconv"
  "strings"
  log "github.com/sirupsen/logrus"
  uj "github.com/nanoscopic/ujsonin/v2/mod"
)

type GIBridge struct {
  onConnect    func( dev BridgeDev ) ProcTracker
  onDisconnect func( dev BridgeDev )
  cli          string
  devs         map[string]*GIDev
  procTracker  ProcTracker
  config       *Config
}

type GIDev struct {
  bridge      *GIBridge
  udid        string
  name        string
  procTracker ProcTracker
  config      *CDevice
}

func NewGIBridge( config *Config, OnConnect func( dev BridgeDev ) (ProcTracker), OnDisconnect func( dev BridgeDev ), goIosPath string, procTracker ProcTracker, detect bool ) BridgeRoot {
  self := &GIBridge{
    onConnect:    OnConnect,
    onDisconnect: OnDisconnect,
    cli:          goIosPath,
    devs:         make( map[string]*GIDev ),
    procTracker:  procTracker,
    config:       config,
  }
  if detect { self.startDetect() }
  return self
}

func (self *GIDev) getUdid() string {
  return self.udid
}

func (self *GIBridge) startDetect() {
  o := ProcOptions{
    procName: "device_trigger",
    binary: self.cli,
    args: []string{ "listen" },
    stderrHandler: func( line string, plog *log.Entry ) {
    },
    stdoutHandler: func( line string, plog *log.Entry ) {
      if strings.HasPrefix( line, "{" ) {
        root, _ := uj.Parse( []byte(line) )
        evType := root.Get("MessageType").String()
        udid := root.Get("Properties.SerialNumber").String()
        if evType == "Attached" {
          //name := root.Get("Properties.Name").String()
          name := "fake name"
          self.OnConnect( udid, name, plog )
        } else if evType == "Detached" {
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

func (self *GIBridge) list() []BridgeDevInfo {
  infos := []BridgeDevInfo{}
  for _,dev := range self.devs {
    infos = append( infos, BridgeDevInfo{ udid: dev.udid } )
  }
  return infos
}

func (self *GIBridge) OnConnect( udid string, name string, plog *log.Entry ) {
  dev := NewGIDev( self, udid, name )
  self.devs[ udid ] = dev
  
  devConfig, hasDevConfig := self.config.devs[ udid ]
  if hasDevConfig {
    dev.config = &devConfig
  }
  
  dev.procTracker = self.onConnect( dev )
}

func (self *GIBridge) OnDisconnect( udid string, plog *log.Entry ) {
  dev := self.devs[ udid ]
  dev.destroy()
  self.onDisconnect( dev )
  delete( self.devs, udid )
}

func (self *GIBridge) destroy() {
  for _,dev := range self.devs {
    dev.destroy()
  }
  // close self processes
}

func NewGIDev( bridge *GIBridge, udid string, name string ) (*GIDev) {
  log.WithFields( log.Fields{
      "type": "gidev_create",
      "udid": censorUuid( udid ),
  } ).Debug( "Creating GIDev" )
  
  var procTracker ProcTracker = nil
  return &GIDev{
    bridge: bridge,
    name: name,
    udid: udid,
    procTracker: procTracker,
  }
}

func (self *GIDev) setProcTracker( procTracker ProcTracker ) {
  self.procTracker = procTracker
}

func (self *GIDev) tunnel( pairs []TunPair, onready func() ) {
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

func (self *GIDev) tunnelOne( pair TunPair, onready func() ) {
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
    binary: self.bridge.cli,
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

func (self *GIBridge) GetDevs( config *Config ) []string {
  json, _ := exec.Command( self.cli,
    []string{ "list" }... ).Output()
  root, _ := uj.Parse( json )
  res := []string{}
  root.Get("deviceList").ForEach( func( dev uj.JNode ) {
      res = append( res, dev.String() )
  } )
  return res
}

func (self *GIDev) GetPid( appname string ) int {
    json, err := exec.Command( self.bridge.cli,
        []string{
            "ps",
        }... ).Output()
      
    if err != nil {
        fmt.Printf("Could not find pid for %s\n", appname )
        return 0
    }
    
    root, _ := uj.Parse( []byte( "{\"procs\":" + string(json) + "}" ) )
    
    pid := 0
    root.Get("procs").ForEach( func( proc uj.JNode ) {
        name := proc.Get("Name").String()
        if name != appname { return }
        pid = proc.Get("Pid").Int()
    } )
    
    fmt.Printf("Found pid %d for %s\n", pid, appname )
    return pid
}

func (self *GIDev) AppInfo( bundleId string ) uj.JNode {
    json, err := exec.Command( self.bridge.cli,
        []string{
            "apps",
        }... ).Output()
      
    if err != nil {
        return nil
    }
    
    root, _ := uj.Parse( []byte( "{\"apps\":" + string(json) + "}" ) )
    
    var node uj.JNode
    root.Get("apps").ForEach( func( app uj.JNode ) {
        //app.Dump()
        biNode := app.Get("CFBundleIdentifier")
        if biNode == nil { return }
        bi := biNode.String()
        if bi != bundleId { return }
        node = app
    } )
    return node
}

func (self *GIDev) InstallApp( appPath string ) bool {
    status, _ := exec.Command( self.bridge.cli,
        []string{
            "install",
            "--path", appPath,
        }... ).Output()
    
    if strings.Contains( string(status), "Installing:100%" ) {
        return true
    }
    return false      
}

func (self *GIDev) info( names []string ) map[string]string {
    mapped := make( map[string]string )
    //fmt.Printf("udid for info: %s\n", self.udid )
    args := []string {
        "info",
        "--udid", self.udid,
    }
    //args = append( args, names... )
    //fmt.Printf("Running %s with args %v\n", self.bridge.cli, args )
    json, _ := exec.Command( self.bridge.cli, args... ).Output()
    //fmt.Printf("json:%s\n",json)
    root, _, err := uj.ParseFull( json )
    if err != nil {
        fmt.Printf("Could not parse json:\n`%s`\n", string(json) )
    }
    
    for _,name := range names {
        node := root.Get(name)
        if node != nil {
            mapped[name] = node.String()
        }
    }
    //fmt.Printf("mapped result:%s\n",mapped)
    
    return mapped
}

func (self *GIDev) gestalt( names []string ) map[string]string {
    mapped := make( map[string]string )
    args := []string{
        "mobilegestalt",
        "--udid", self.udid,
    }
    args = append( args, names... )
    fmt.Printf("Running %s %s\n", self.bridge.cli, args );
    json, _ := exec.Command( self.bridge.cli, args... ).Output()
    fmt.Printf("json:%s\n",json)
    root, _ := uj.Parse( json )
    
    data := root.Get("Diagnostics").Get("MobileGestalt")
    for _,name := range names {
        node := data.Get(name)
        if node != nil {
            mapped[name] = node.String()
        }
    }
    
    return mapped
}

func (self *GIDev) gestaltnode( names []string ) map[string]uj.JNode {
    mapped := make( map[string]uj.JNode )
    args := []string{
        "mobilegestalt",
        "--udid", self.udid,
    }
    args = append( args, names... )
    fmt.Printf("Running %s %s\n", self.bridge.cli, args );
    json, _ := exec.Command( self.bridge.cli, args... ).Output()
    fmt.Printf("json:%s\n",json)
    root, _ := uj.Parse( json )
    
    data := root.Get("Diagnostics").Get("MobileGestalt")
    for _,name := range names {
        node := data.Get(name)
        if node != nil {
            mapped[name] = node
        }
    }
    
    return mapped
}

func (self *GIDev) ps() []iProc {
    return []iProc{}
}

func (self *GIDev) screenshot() Screenshot {
    return Screenshot{}
}

//type BackupVideo struct {
//    port int
//    spec string
//    imgId int
//}

func (self *GIDev) NewSyslogMonitor( handleLogItem func( uj.JNode ) ) {
    o := ProcOptions{
        procName: "syslogMonitor",
        binary: self.bridge.cli,
        args: []string {
            "syslog",
            "--udid", self.udid,
            //"proc", "SpringBoard(SpringBoard)",
            //"proc", "SpringBoard(FrontBoard)",
            //"proc", "dasd",
        },
        startFields: log.Fields{
            "udid": self.udid,
        },
        stdoutHandler: func( line string, plog *log.Entry ) {
            root, _, err := uj.ParseFull( []byte( line ) )
            if err == nil {
                msg := root.Get("msg").String()
                self.handleLogLine( msg, handleLogItem )
            } else {
                fmt.Printf("Could not parse:[%s]\n", line )
            }
        },
    }
    
    proc_generic( self.procTracker, nil, &o )
}

func (self *GIDev) handleLogLine( msg string, handleLogItem func( uj.JNode ) ) {
    
}

func (self *GIDev) NewBackupVideo( port int, onStop func( interface{} ) ) ( *BackupVideo ) {
    vid := &BackupVideo{
        port: port,
        spec: fmt.Sprintf( "http://127.0.0.1:%d/frame", port ),
    }
    
    o := ProcOptions{
        procName: "backupVideo",
        binary: self.bridge.cli,
        args: []string {
            "server",
            "--port", strconv.Itoa( port ),
        },
        startFields: log.Fields{
            "port": strconv.Itoa( port ),
        },
        onStop: func( wrapper interface{} ) {
            onStop( wrapper )
        },
        stdoutHandler: func( line string, plog *log.Entry ) {            
        },
        stderrHandler: func( line string, plog *log.Entry ) {
        },
    }
        
    proc_generic( self.procTracker, nil, &o )
    
    return vid
}

func (self *GIDev) wda( onStart func(), onStop func(interface{}) ) {
    f, err := os.OpenFile("wda.log",
        os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        log.WithFields( log.Fields{
            "type": "wda_log_fail",
        } ).Fatal("Could not open wda.log for writing")
    }
    
    config := self.bridge.config
    biPrefix := config.wdaPrefix
    bi := fmt.Sprintf( "%s.WebDriverAgentRunner.xctrunner", biPrefix )
    
    args := []string{
        "runwda",
        "--bundleid", bi,
        "--testrunnerbundleid", bi,
        "--xctestconfig", "WebDriverAgentRunner.xctest",
        "--udid", self.udid,
    }
    
    fmt.Fprintf( f, "Starting WDA via %s with args %s\n", "bin/go-ios", strings.Join( args, " " ) )
    fmt.Printf( "Starting WDA via %s with args %s\n", "bin/go-ios", strings.Join( args, " " ) )
    
    o := ProcOptions {
        procName: "wda",
        binary: self.bridge.cli,
        args: args,
        stdoutHandler: func( line string, plog *log.Entry ) {
            if strings.Contains( line, "configuration is unsupported" ) {
                plog.Println( line )
            }
            fmt.Fprintf( f, "runwda: %s\n", line )
        },
        stderrHandler: func( line string, plog *log.Entry ) {
            if strings.Contains(line, "NNG Ready") {
                plog.WithFields( log.Fields{
                    "type": "wda_start",
                    "uuid": censorUuid(self.udid),
                } ).Info("[WDA] successfully started")
                onStart()
            }
            if strings.Contains( line, "configuration is unsupported" ) {
                plog.Println( line )
            }
            fmt.Fprintf( f, "runwda: %s\n", line )
        },
        onStop: func( wrapper interface{} ) {
            onStop( wrapper )
        },
    }
    
    proc_generic( self.procTracker, nil, &o )
}

func (self *GIDev) destroy() {
  // close running processes
}