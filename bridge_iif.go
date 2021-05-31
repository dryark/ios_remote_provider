package main

import (
    "fmt"
    uj "github.com/nanoscopic/ujsonin/v2/mod"
    log "github.com/sirupsen/logrus"
    "os"
    "os/exec"
    "strings"
    "strconv"
    //"time"
    "go.nanomsg.org/mangos/v3"
    nanoReq  "go.nanomsg.org/mangos/v3/protocol/req"
)

type IIFBridge struct {
  onConnect func( dev BridgeDev ) ProcTracker
  onDisconnect func( dev BridgeDev )
  cli string
  devs map[string]*IIFDev
  procTracker ProcTracker
  config *Config
}

type IIFDev struct {
  bridge *IIFBridge
  udid string
  name string
  procTracker ProcTracker
}

// IosIF bridge
func NewIIFBridge( config *Config, OnConnect func( dev BridgeDev ) (ProcTracker), OnDisconnect func( dev BridgeDev ), iosIfPath string, procTracker ProcTracker, detect bool ) ( *IIFBridge ) {
  self := &IIFBridge{
    onConnect: OnConnect,
    onDisconnect: OnDisconnect,
    cli: iosIfPath,
    devs: make( map[string]*IIFDev ),
    procTracker: procTracker,
    config: config,
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
  dev := NewIIFDev( self, udid, name )
  self.devs[ udid ] = dev
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

func NewIIFDev( bridge *IIFBridge, udid string, name string ) (*IIFDev) {
  fmt.Printf("Creating IIFDev with udid=%s\n", udid )
  var procTracker ProcTracker = nil
  return &IIFDev{
    bridge: bridge,
    name: name,
    udid: udid,
    procTracker: procTracker,
  }
}

func (self *IIFDev) setProcTracker( procTracker ProcTracker ) {
  self.procTracker = procTracker
}

func (self *IIFDev) tunnel( pairs []TunPair, onready func() ) {
  tunName := "tunnel"
  specs := []string{}
  for _,pair := range pairs {
    tunName = fmt.Sprintf( "%s_%d->%d", tunName, pair.from, pair.to )
    specs = append( specs, fmt.Sprintf("%d:%d",pair.from,pair.to) )
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
      //fmt.Println( "tunnel:%s", line )
      if strings.Contains( line, "Ready" ) {
        if onready != nil {
          onready()
        }
      }
    },
    stderrHandler: func( line string, plog *log.Entry ) {
      fmt.Println( "tunnel err:%s", line )
    },
    onStop: func( interface{} ) {
      log.Println("%s stopped", tunName)
    },
  }
  proc_generic( self.procTracker, nil, &o )
}

func GetDevs( config *Config ) []string {
  json, _ := exec.Command( config.iosIfPath,
    []string{ "list", "-json" }... ).Output()
  root, _ := uj.Parse( []byte( "[" + string(json) + "]" ) )
  res := []string{}
  root.ForEach( func( dev uj.JNode ) {
      res = append( res, dev.Get("udid").String() )
  } )
  return res
}

func (self *IIFDev) GetPid( appname string ) int {
  json, err := exec.Command( self.bridge.cli,
    []string{
      "ps",
      "-raw",
      "-appname", appname,
    }... ).Output()
    
  if err != nil {
    return 0
  }
  
  json = []byte( strings.ReplaceAll( string( json ), "i16.", "" ) );
  root, _ := uj.Parse( json )
  pidNode := root.Get("pid")
  if pidNode == nil { return 0 }
  return pidNode.Int()
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
  fmt.Printf("udid for info: %s\n", self.udid )
  args := []string {
    "info",
    "-json",
    "-id", self.udid,
  }
  args = append( args, names... )
  json, _ := exec.Command( self.bridge.cli, args... ).Output()
  fmt.Printf("json:%s\n",json)
  root, _ := uj.Parse( json )
  
  for _,name := range names {
    node := root.Get(name)
    if node != nil {
      mapped[name] = node.String()
    }
  }
  fmt.Printf("mapped result:%s\n",mapped)
  
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

type BackupVideo struct {
    port int
    sock mangos.Socket
    spec string
    imgId int
}

func (self *IIFDev) NewSyslogMonitor( handleLogItem func( uj.JNode ) ) {
    bufstr := ""
    toFetch := 0
    o := ProcOptions{
        procName: "syslogMonitor",
        binary: self.bridge.cli,
        args: []string {
            "log",
            "-id", self.udid,
            "proc", "SpringBoard(SpringBoard)",
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
                        handleLogItem( root )
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
                        handleLogItem( root )
                    } else {
                        fmt.Printf("Could not parse:[%s]\n", bufstr )
                    }
                }
                
            }
        },
    }
    
    proc_generic( self.procTracker, nil, &o )
}

func (self *IIFDev) NewBackupVideo( port int, onStop func( interface{} ) ) ( *BackupVideo ) {
    vid := &BackupVideo{
        port: port,
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
                vid.openBackupStream()
            }
            fmt.Println( line )
        },
        stderrHandler: func( line string, plog *log.Entry ) {
            fmt.Println( line )
        },
    }
        
    proc_generic( self.procTracker, nil, &o )
    
    return vid
}

func (self *BackupVideo) openBackupStream() {
    var err error
    
    self.spec = fmt.Sprintf( "tcp://127.0.0.1:%d", self.port )
    
    if self.sock, err = nanoReq.NewSocket(); err != nil {
        log.WithFields( log.Fields{
            "type":     "err_socket_new",
            "err":      err,
        } ).Error("Backup video Socket new error")
        return
    }
    
    if err = self.sock.Dial( self.spec ); err != nil {
        log.WithFields( log.Fields{
            "type": "err_socket_dial",
            "spec": self.spec,
            "err":  err,
        } ).Error("Backup video Socket dial error")
        return
    }
    
    self.sock.SetOption( mangos.OptionMaxRecvSize, 3000000 )
}

func (self *BackupVideo) GetFrame() []byte {
    self.sock.Send([]byte( fmt.Sprintf("img:%d",self.imgId) ) )
    self.imgId++
    
    msg, err := self.sock.RecvMsg()
    if err != nil {
        log.WithFields( log.Fields{
            "type":     "err_socket_recv",
            "zmq_spec": self.spec,
            "err":      err,
        } ).Info("Backup video recv err")
    }
    
    return msg.Body
}

func (self *IIFDev) wda( port int, onStart func(), onStop func(interface{}) ) {
    config := self.bridge.config
    method := config.wdaMethod
    
    if method == "go-ios" {
        self.wdaGoIos( port, onStart, onStop )
    } else if method == "tidevice" {
        self.wdaTidevice( port, onStart, onStop )
    } else {
        fmt.Printf("Unknown wda start method %s\n", method )
        os.Exit(1)
    }
}

func (self *IIFDev) wdaGoIos( port int, onStart func(), onStop func(interface{}) ) {
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
    
    o := ProcOptions {
        procName: "wda",
        binary: "bin/go-ios",
        args: []string{
            "runwda",
            "--bundleid", bi,
            "--testrunnerbundleid", bi,
            "--xctestconfig", "WebDriverAgentRunner.xctest",
        },
        stdoutHandler: func( line string, plog *log.Entry ) {
            if strings.Contains(line, "Test Case '-[UITestingUITests testRunner]' started") {
                plog.WithFields( log.Fields{
                    "type": "wda_start",
                    "uuid": censorUuid(self.udid),
                    "port": port,
                } ).Info("[WDA] successfully started")
                onStart()
            }
            if strings.Contains( line, "configuration is unsupported" ) {
                plog.Println( line )
            }
            fmt.Fprintln( f, line )
        },
        stderrHandler: func( line string, plog *log.Entry ) {
            if strings.Contains( line, "configuration is unsupported" ) {
                plog.Println( line )
                fmt.Fprintln( f, line )
            }
        },
        onStop: func( wrapper interface{} ) {
            onStop( wrapper )
        },
    }
    
    proc_generic( self.procTracker, nil, &o )
}

func (self *IIFDev) wdaTidevice( port int, onStart func(), onStop func(interface{}) ) {
    config := self.bridge.config
    tiPath := config.tidevicePath
    
    log.WithFields( log.Fields{
        "type":  "tidevice_path_unset",
    } ).Fatal("tidevice path is unknown. Run `make usetidevice` to correct")
    
    f, err := os.OpenFile("wda.log",
        os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        log.WithFields( log.Fields{
            "type": "wda_log_fail",
        } ).Fatal("Could not open wda.log for writing")
    }
    
    biPrefix := config.wdaPrefix
    bi := fmt.Sprintf( "%s.WebDriverAgentRunner.xctrunner", biPrefix )
    
    o := ProcOptions {
        procName: "wda",
        binary: tiPath,
        args: []string{
            "wdaproxy",
            "-B", bi,
            "-p", "0",
        },
        stderrHandler: func( line string, plog *log.Entry ) {
            if strings.Contains(line, "WebDriverAgent start successfully") {
                plog.WithFields( log.Fields{
                    "type": "wda_start",
                    "uuid": censorUuid(self.udid),
                    "port": port,
                } ).Info("[WDA] successfully started")
                onStart()
            }
            if strings.Contains( line, "have to mount the Developer disk image" ) {
                plog.WithFields( log.Fields{
                    "type": "wda_start_err",
                    "uuid": censorUuid(self.udid),
                    "port": port,
                } ).Fatal("[WDA] Developer disk not mounted. Cannot start WDA")
            }
            if strings.Contains( line, "'No app matches'" ) {
                plog.WithFields( log.Fields{
                    "type": "wda_start_err",
                    "uuid": censorUuid(self.udid),
                    "port": port,
                    "rawErr": line,
                } ).Fatal("[WDA] Incorrect WDA bundle id")
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