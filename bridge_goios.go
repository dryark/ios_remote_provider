package main

import (
    "fmt"
    //"io"
    //"net"
    "os"
    "os/exec"
    "regexp"
    "strconv"
    "strings"
    "time"
    log "github.com/sirupsen/logrus"
    uj "github.com/nanoscopic/ujsonin/v2/mod"
    "github.com/danielpaulus/go-ios/ios"
    syslog "github.com/danielpaulus/go-ios/ios/syslog"
    screenshotr "github.com/danielpaulus/go-ios/ios/screenshotr"
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
    device      *Device
    goIosDevice ios.DeviceEntry
    logStopChan chan bool
    rx          *regexp.Regexp
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

func listenForDevices( stopChan chan bool, onConnect func( string, ios.DeviceEntry ), onDisconnect func( string ) ) {
    go func() {
        exit := false
        for {
            deviceConn, err := ios.NewDeviceConnection(ios.DefaultUsbmuxdSocket)
            defer deviceConn.Close()
            if err != nil {
                log.Errorf("could not connect to %s with err %+v, will retry in 3 seconds...", ios.DefaultUsbmuxdSocket, err)
                time.Sleep(time.Second * 3)
                continue
            }
            muxConnection := ios.NewUsbMuxConnection(deviceConn)
            
            attachedReceiver, err := muxConnection.Listen()
            if err != nil {
                log.Error("Failed issuing Listen command, will retry in 3 seconds", err)
                deviceConn.Close()
                time.Sleep(time.Second * 3)
                continue
            }
            for {
                select {
                    case <- stopChan:
                        exit = true
                        break
                    default:
                }
                if exit { break }
              
                msg, err := attachedReceiver()
                if err != nil {
                    log.Error("Stopped listening because of error")
                    break
                }
                
                if msg.MessageType == "Attached" {
                    udid := msg.Properties.SerialNumber
                    goIosDevice, _ := ios.GetDevice( udid )
                    onConnect( udid, goIosDevice )
                } else if msg.MessageType == "Detached" {
                    onDisconnect( msg.Properties.SerialNumber )
                }
            }
            if exit { break }
        }
    }()
}

func (self *GIBridge) startDetect() {
    stopChan := make( chan bool )
    listenForDevices( stopChan,
        func( id string, goIosDevice ios.DeviceEntry ) {
            self.OnConnect( id, "fake name", nil, goIosDevice )
        },
        func( id string ) {
            self.OnDisconnect( id, nil )
        })
}

func (self *GIBridge) list() []BridgeDevInfo {
    infos := []BridgeDevInfo{}
    for _,dev := range self.devs {
        infos = append( infos, BridgeDevInfo{ udid: dev.udid } )
    }
    return infos
}

func (self *GIBridge) OnConnect( udid string, name string, plog *log.Entry, goIosDevice ios.DeviceEntry ) {
    dev := NewGIDev( self, udid, name, nil )
    dev.goIosDevice = goIosDevice
    self.devs[ udid ] = dev
    
    devConfig, hasDevConfig := self.config.devs[ udid ]
    if hasDevConfig {
        dev.config = &devConfig
    }
    
    dev.procTracker = self.onConnect( dev )
}

func (self *GIBridge) OnDisconnect( udid string, plog *log.Entry ) {
    dev, tracked := self.devs[ udid ]
    if !tracked { return }
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

func NewGIDev( bridge *GIBridge, udid string, name string, device *Device ) (*GIDev) {
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
        device: device,
    }
}

func (self *GIDev) setProcTracker( procTracker ProcTracker ) {
    self.procTracker = procTracker
}

func (self *GIDev) tunnel( pairs []TunPair, onready func() ) {
    tunnelMethod := self.config.tunnelMethod
    if tunnelMethod == "go-ios" {
        self.tunnelGoIos( pairs, onready )
    } else if tunnelMethod == "iosif" {
        self.tunnelIosif( pairs, onready )
    }
}

func (self *GIDev) tunnelGoIos( pairs []TunPair, onready func() ) {
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
                //fmt.Printf( "tunnel err:%s\n", line )
            }
        },
        onStop: func( interface{} ) {
            log.Printf("%s stopped\n", tunName)
        },
    }
    proc_generic( self.procTracker, nil, &o )
}

func (self *GIDev) tunnelIosif( pairs []TunPair, onready func() ) {
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
  fmt.Printf("Starting %s with %s\n", "bin/iosif", args )
  
  o := ProcOptions{
    procName: tunName,
    binary: "bin/iosif",
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

/*func (self *GIDev) tunnel( pairs []TunPair, onready func() ) {
    for _,pair := range( pairs ) {
        l, err := net.Listen( "tcp", fmt.Sprintf( "0.0.0.0:%d", pair.from ) )
        if err != nil { continue }
        fmt.Printf("Listening on port %d ( to %d )\n", pair.from, pair.to )
        
        to := pair.to
        from := pair.from
        go func() {
            for {
                conn, err := l.Accept()
                if err != nil { continue }
                fmt.Printf("Incoming connection to port %d ( to %d )\n", from, to )
                
                beginIosProxy( conn, self.goIosDevice.DeviceID, uint16(to) )
            }
        }()
    }
    time.Sleep( time.Second )
    onready()
}

func beginIosProxy( hostConn net.Conn, deviceID int, phonePort uint16 ) {
    mux, err := ios.NewUsbMuxConnectionSimple()
    if err != nil {
        hostConn.Close()
        return
    }
    err = mux.Connect( deviceID, phonePort )
    if err != nil {
        fmt.Printf("Failed to connect to device port %d\n", phonePort )
        hostConn.Close()
        return
    }
    fmt.Printf("Connected to device port %d\n", phonePort )
    
    deviceConn := mux.ReleaseDeviceConnection()

    go func() { io.Copy( hostConn           , deviceConn.Reader() ) }()
    go func() { io.Copy( deviceConn.Writer(), hostConn            ) }()
}*/

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

func (self *GIDev) GetPid( appname string ) uint64 {
    json, err := exec.Command( self.bridge.cli,
        []string{
            "ps",
            "--udid", self.udid,
        }... ).Output()
      
    if err != nil {
        fmt.Printf("Could not find pid for %s; err=%s, json=%s\n", appname, err, json )
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
    return uint64( pid )
}

func (self *GIDev) Kill( pid uint64 ) {
    fmt.Printf("Killing process id %d\n", pid )
    
    exec.Command( self.bridge.cli,
        []string{
            "killid", fmt.Sprintf("%d", pid ),
            "--udid", self.udid,
        }...
    ).Output()
}

func (self *GIDev) AppInfo( bundleId string ) uj.JNode {
    json, err := exec.Command( self.bridge.cli,
        []string{
            "apps",
            "--udid", self.udid,
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
            "--udid", self.udid,
        }... ).Output()
    
    if strings.Contains( string(status), "Installing:100%" ) {
        return true
    }
    return false      
}

func (self *GIDev) LaunchApp( bundleId string ) bool {
    output, _ := exec.Command( self.bridge.cli,
        []string{
            "launch",
            bundleId,
            "--udid", self.udid,
        }... ).Output()
    if strings.Contains( string(output), "msg\":\"Process launched" ) {
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

func (self *GIDev) NewSyslogMonitor( handleLogItem func( msg string, app string ) ) {
    self.logStopChan = make( chan bool )
    self.rx = regexp.MustCompile(`\\u[0-9a-fA-F]{4}`)
    go func() {
        syslogConnection, err := syslog.New( self.goIosDevice )
        if err != nil {
            fmt.Printf("Error monitoring device syslog\n")
            return
        }
        defer syslogConnection.Close()
        
        exit := false
        n := 0
        for {
            n++
            if ( n % 5 == 0 ) {
                select {
                    case <- self.logStopChan:
                        exit = true
                        break
                    default:
                }
                if exit { break }
            }
            
            logMessage, err := syslogConnection.ReadLogMessage()
            if err != nil { continue }
            self.handleLogLine( logMessage, handleLogItem )
        }
    }()
}

func (self *GIDev) handleLogLine( msg string, handleLogItem func( msg string, app string ) ) {
    // Aug 28 01:29:25 iPhone kernel(AppleT8101)[0] \u003cNotice\u003e:
    // Aug 28 01:29:25 iPhone kernel(AppleARMPlatform)[0] \u003cNotice\u003e:
    // Aug 28 01:29:25 iPhone locationd[66] \u003cNotice\u003e:
    // 01234567890123456
    //namePos := 16
    fromName := msg[16:] // iPhone kernel(AppleARMPlatform)[0] \u003cNotice\u003e:
    nameEndPos := strings.IndexRune( fromName, ' ' )
    fromCtx := fromName[nameEndPos+1:] // kernel(AppleARMPlatform)[0] \u003cNotice\u003e:
    ctxEndPos := strings.IndexRune( fromCtx, '[' )
    ctx := fromCtx[:ctxEndPos] // kernel(AppleARMPlatform)
    afterCtx := fromCtx[ctxEndPos:] // [0] \u003cNotice\u003e:
    typePos := strings.IndexRune( afterCtx, 'c' )
    fromType := afterCtx[ typePos+1: ] // Notice\u003e:
    //typeEndPos := strings.IndexRune( fromType, '\\' )
    //msgType := fromType[:typeEndPos-1] // Notice
    restPos := strings.IndexRune( fromType, ':' )
    rest := fromType[restPos+2:]
    //fmt.Printf("Log ctx[%s] rest:%s\n", ctx, rest )
    
    //rx := regexp.MustCompile(`\\u[0-9a-fA-F]{4}`)
    rest = self.rx.ReplaceAllStringFunc( rest, func( str string ) string {
        str = str[2:]
        num, _ := strconv.ParseInt(str, 16, 64)
        res := string( rune( num ) )
        //fmt.Printf("converting %s to %s\n", str, res )
        return res
    } )
    
    handleLogItem( rest, ctx )
}

type BackupVideoGI struct {
    giDev *GIDev
    shotService *screenshotr.Connection
}

func (self *GIDev) NewBackupVideo( port int, onStop func( interface{} ) ) BackupVideo {
    vid := &BackupVideoGI{
        giDev: self,
    }
    shotService, err := screenshotr.New( self.goIosDevice )
    if err != nil {
    }
    vid.shotService = shotService
    
    return vid
}

func (self *BackupVideoGI) GetFrame() []byte {
    imageBytes, err := self.shotService.TakeScreenshot()
    if err != nil {
        return []byte{}
    }
    return imageBytes
}

func (self *GIDev) cfa( onStart func(), onStop func(interface{}) ) {
    if self.config == nil {
        self.cfaGoIos( onStart, onStop )
    } else {
        devCfaMethod := self.config.cfaMethod
        if devCfaMethod != "" {
            if devCfaMethod == "tidevice" {
                self.cfaTidevice( onStart, onStop )
            } else if devCfaMethod == "iosif" {
                self.cfaIosif( onStart, onStop )
            } else if devCfaMethod == "manual" {
                onStart()
            } else {
                self.cfaGoIos( onStart, onStop )
            }
        } else {
            self.cfaGoIos( onStart, onStop )
        }
    }
}
func (self *GIDev) wda( onStart func(), onStop func(interface{}) ) {
    if self.config == nil {
        //self.wdaGoIos( onStart, onStop )
    } else {
        devWdaMethod := self.config.wdaMethod
        if devWdaMethod != "" {
            if devWdaMethod == "tidevice" {
                self.wdaTidevice( onStart, onStop )
            } else if devWdaMethod == "iosif" {
                self.wdaIosif( onStart, onStop )
            } else if devWdaMethod == "manual" {
                onStart()
            } else if devWdaMethod == "go-ios" {
                self.wdaGoIos( onStart, onStop )
            }
        }
    }
}

func (self *GIDev) cfaGoIos( onStart func(), onStop func(interface{}) ) {
    f, err := os.OpenFile("cfa.log",
        os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        log.WithFields( log.Fields{
            "type": "cfa_log_fail",
        } ).Fatal("Could not open cfa.log for writing")
    }
    
    config := self.bridge.config
    biPrefix := config.cfaPrefix
    bi := fmt.Sprintf( "%s.CFAgent.xctrunner", biPrefix )
    
    args := []string{
        "runwda",
        "--bundleid", bi,
        "--testrunnerbundleid", bi,
        "--xctestconfig", "CFAgent.xctest",
        "--udid", self.udid,
    }
    
    fmt.Fprintf( f, "Starting CFA via %s with args %s\n", "bin/go-ios", strings.Join( args, " " ) )
    fmt.Printf( "Starting CFA via %s with args %s\n", "bin/go-ios", strings.Join( args, " " ) )
    
    o := ProcOptions {
        procName: "cfa",
        binary: self.bridge.cli,
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
            if strings.Contains( line, "Unable to launch" ) && strings.Contains( line, "invalid code signature" ) {
                args := []string{
                    "install",
                    "--path", "bin/cfa/Debug-iphoneos/CFAgent-Runner.app",
                    "--udid", self.udid,
                }
                //args = append( args, names... )
                fmt.Printf("Running %s %s\n", self.bridge.cli, args );
                /*json, _ := */exec.Command( self.bridge.cli, args... ).Output()
            }
            fmt.Fprintf( f, "runcfa: %s\n", line )
        },
        onStop: func( wrapper interface{} ) {
            onStop( wrapper )
        },
    }
    
    proc_generic( self.procTracker, nil, &o )
}

func (self *GIDev) wdaGoIos( onStart func(), onStop func(interface{}) ) {
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
            if strings.Contains(line, "ServerURLHere") {
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

func (self *GIDev) cfaTidevice( onStart func(), onStop func(interface{}) ) {
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
    bi := fmt.Sprintf( "%s.CFAgent.xctrunner", biPrefix )
    
    args := []string{
        "-u", self.udid,
        "xctest",
        "-B", bi,
    }
    
    fmt.Fprintf( f, "Starting CFA via %s with args %s\n", tiPath, strings.Join( args, " " ) )
    fmt.Printf( "Starting CFA via %s with args %s\n", tiPath, strings.Join( args, " " ) )
    
    o := ProcOptions {
        procName: "cfa",
        binary: tiPath,
        args: args,
        stderrHandler: func( line string, plog *log.Entry ) {
            if strings.Contains(line, " pid: ") {
                plog.WithFields( log.Fields{
                    "type": "cfa_start",
                    "uuid": censorUuid(self.udid),
                } ).Info("[CFA] successfully started - waiting 5 seconds")
                time.Sleep( time.Second * 5 )
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

func (self *GIDev) wdaTidevice( onStart func(), onStop func(interface{}) ) {
    config := self.bridge.config
    tiPath := config.tidevicePath
    
    if tiPath == "" {
        log.WithFields( log.Fields{
            "type":  "tidevice_path_unset",
        } ).Fatal("tidevice path is unknown. Run `make usetidevice` to correct")
    }
    
    f, err := os.OpenFile("wda.log",
        os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        log.WithFields( log.Fields{
            "type": "wda_log_fail",
        } ).Fatal("Could not open wda.log for writing")
    }
    
    biPrefix := config.cfaPrefix
    bi := fmt.Sprintf( "%s.WebDriverAgentRunner.xctrunner", biPrefix )
    
    args := []string{
        "-u", self.udid,
        "xctest",
        "-B", bi,
    }
    
    fmt.Fprintf( f, "Starting WDA via %s with args %s\n", tiPath, strings.Join( args, " " ) )
    fmt.Printf( "Starting WDA via %s with args %s\n", tiPath, strings.Join( args, " " ) )
    
    o := ProcOptions {
        procName: "wda",
        binary: tiPath,
        args: args,
        stderrHandler: func( line string, plog *log.Entry ) {
            if strings.Contains(line, " pid: ") {
                plog.WithFields( log.Fields{
                    "type": "cfa_start",
                    "uuid": censorUuid(self.udid),
                } ).Info("[WDA] successfully started - waiting 5 seconds")
                time.Sleep( time.Second * 5 )
                onStart()
            }
            if strings.Contains( line, "have to mount the Developer disk image" ) {
                plog.WithFields( log.Fields{
                    "type": "wda_start_err",
                    "uuid": censorUuid(self.udid),
                } ).Fatal("[WDA] Developer disk not mounted. Cannot start WDA")
            }
            if strings.Contains( line, "'No app matches'" ) {
                plog.WithFields( log.Fields{
                    "type": "wda_start_err",
                    "uuid": censorUuid(self.udid),
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

func (self *GIDev) cfaIosif( onStart func(), onStop func(interface{}) ) {
    f, err := os.OpenFile("cfa.log",
        os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        log.WithFields( log.Fields{
            "type": "cfa_log_fail",
        } ).Fatal("Could not open cfa.log for writing")
    }
    
    config := self.bridge.config
    iosIfPath := config.iosIfPath
    biPrefix := config.cfaPrefix
    bi := fmt.Sprintf( "%s.CFAgent", biPrefix )
    
    args := []string{
        "xctest",
        bi,
        "-id", self.udid,
    }
    
    fmt.Fprintf( f, "Starting CFA via %s with args %s\n", iosIfPath, strings.Join( args, " " ) )
    fmt.Printf( "Starting CFA via %s with args %s\n", iosIfPath, strings.Join( args, " " ) )
    
    o := ProcOptions {
        procName: "cfa",
        binary: "./" + iosIfPath,
        args: args,
        stderrHandler: func( line string, plog *log.Entry ) {
            /*if strings.Contains( line, "configuration is unsupported" ) {
                plog.Println( line )
            }*/
            fmt.Fprintf( f, "runcfa: %s\n", line )
        },
        stdoutHandler: func( line string, plog *log.Entry ) {
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
            //fmt.Printf( "runcfa: %s\n", line )
            fmt.Fprintf( f, "runcfa: %s\n", line )
        },
        onStop: func( wrapper interface{} ) {
            onStop( wrapper )
        },
    }
    
    proc_generic( self.procTracker, nil, &o )
}

func (self *GIDev) wdaIosif( onStart func(), onStop func(interface{}) ) {
    f, err := os.OpenFile("wda.log",
        os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        log.WithFields( log.Fields{
            "type": "wda_log_fail",
        } ).Fatal("Could not open wda.log for writing")
    }
    
    config := self.bridge.config
    iosIfPath := config.iosIfPath
    biPrefix := config.wdaPrefix
    bi := fmt.Sprintf( "%s.WebDriverAgentRunner", biPrefix )
    
    args := []string{
        "xctest",
        bi,
        "-id", self.udid,
    }
    
    fmt.Fprintf( f, "Starting WDA via %s with args %s\n", iosIfPath, strings.Join( args, " " ) )
    fmt.Printf( "Starting WDA via %s with args %s\n", iosIfPath, strings.Join( args, " " ) )
    
    o := ProcOptions {
        procName: "wda",
        binary: "./" + iosIfPath,
        args: args,
        stderrHandler: func( line string, plog *log.Entry ) {
            /*if strings.Contains( line, "configuration is unsupported" ) {
                plog.Println( line )
            }*/
            fmt.Fprintf( f, "runwda: %s\n", line )
        },
        stdoutHandler: func( line string, plog *log.Entry ) {
            if strings.Contains(line, "ServerURLHere") {
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
    self.logStopChan <- true
}

func (self *GIDev) SetConfig( config *CDevice ) {
    self.config = config
}

func (self *GIDev) SetDevice( device *Device ) {
    self.device = device
}