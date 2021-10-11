package main

import (
    "fmt"
    "os"
    "os/signal"
    //"runtime/pprof"
    "strings"
    "syscall"
    "time"
    log "github.com/sirupsen/logrus"
    uc "github.com/nanoscopic/uclop/mod"
    "github.com/danielpaulus/go-ios/ios"
)

func main() {
    uclop := uc.NewUclop()
    commonOpts := uc.OPTS{
        uc.OPT("-debug","Use debug log level",uc.FLAG),
        uc.OPT("-warn","Use warn log level",uc.FLAG),
        uc.OPT("-config","Config file to use",0),
        uc.OPT("-defaults","Defaults config file to use",0),
        uc.OPT("-calculated","Path to calculated JSON values",0),
        uc.OPT("-cpuprofile","Output cpu profile data",uc.FLAG),
    }
    
    idOpt := uc.OPTS{
        uc.OPT("-id","Udid of device",0),
    }
    
    runOpts := append( commonOpts,
        idOpt[0],
        uc.OPT("-nosanity","Skip sanity checks",uc.FLAG),
    )
    
    uclop.AddCmd( "run", "Run ControlFloor", runMain, runOpts )
    uclop.AddCmd( "register", "Register against ControlFloor", runRegister, commonOpts )
    uclop.AddCmd( "cleanup", "Cleanup leftover processes", runCleanup, nil )

    //uclop.AddCmd( "wda",       "Just run WDA",                     runWDA,        idOpt )
    uclop.AddCmd( "cfa",       "Just run CFA",                     runCFA,        idOpt )
    uclop.AddCmd( "winsize",   "Get device window size",           runWindowSize, idOpt )
    
    sourceOpts := append( idOpt,
        uc.OPT("-bi","Bundle ID",0),
    )
    uclop.AddCmd( "source",    "Get device xml source",            runSource,     sourceOpts )
        
    uclop.AddCmd( "alertinfo", "Get alert info",                   runAlertInfo,  idOpt )
    uclop.AddCmd( "islocked",  "Check if device screen is locked", runIsLocked,   idOpt )
    uclop.AddCmd( "unlock",    "Unlock device screen",             runUnlock,     idOpt )
    uclop.AddCmd( "listen",    "Test listening for devices",       runListen,     commonOpts )
    
    clickButtonOpts := append( idOpt,
        uc.OPT("-label","Button label",uc.REQ),
        uc.OPT("-system","System element",uc.FLAG),
    )
    uclop.AddCmd( "clickEl", "Click a named element", runClickEl, clickButtonOpts )
    uclop.AddCmd( "forceTouchEl", "Force touch a named element", runForceTouchEl, clickButtonOpts )
    uclop.AddCmd( "longTouchEl", "Long touch a named element", runLongTouchEl, clickButtonOpts )
    uclop.AddCmd( "addRec", "Add Recording to Control Center", runAddRec, idOpt )
    
    appAtOpts := append( idOpt,
        uc.OPT("-x","X",0),
        uc.OPT("-y","Y",0),
    )
    uclop.AddCmd( "appAt", "App at point", runAppAt, appAtOpts )
    
    runAppOpts := append( idOpt,
        uc.OPT("-name","App name",uc.REQ),
    )
    uclop.AddCmd( "runapp", "Run named app", runRunApp, runAppOpts )
        
    uclop.AddCmd( "vidtest", "Test backup video", runVidTest, idOpt ) 
    
    uclop.Run()
}

func cfaForDev( id string ) (*CFA,*DeviceTracker,*Device) {
    config := NewConfig( "config.json", "default.json", "calculated.json" )
    
    tracker := NewDeviceTracker( config, false, []string{} )
    
    devs := tracker.bridge.GetDevs( config )
    dev1 := id
    if id == "" {
        dev1 = devs[0]
    }
    fmt.Printf("Dev id: %s\n", dev1)
    
    var bridgeDev BridgeDev
    if config.bridge == "go-ios" {
        bridgeDev = NewGIDev( tracker.bridge.(*GIBridge), dev1, "x", nil )
    } else {
        bridgeDev = NewIIFDev( tracker.bridge.(*IIFBridge), dev1, "x", nil )
    }
    dev := NewDevice( config, tracker, dev1, bridgeDev )
    bridgeDev.SetDevice( dev )
    
    devConfig, hasDevConfig := config.devs[ dev1 ]
    if hasDevConfig {
        bridgeDev.SetConfig( &devConfig )
    }
    
    bridgeDev.setProcTracker( tracker )
    cfa := NewCFANoStart( config, tracker, dev )
    return cfa,tracker,dev
}

func vidTestForDev( id string ) (*DeviceTracker) {
    config := NewConfig( "config.json", "default.json", "calculated.json" )
    
    tracker := NewDeviceTracker( config, false, []string{} )
    
    devs := tracker.bridge.GetDevs( config )
    dev1 := id
    if id == "" {
        dev1 = devs[0]
    }
    fmt.Printf("Dev id: %s\n", dev1)
    
    var bridgeDev BridgeDev
    if config.bridge == "go-ios" {
        bridgeDev = NewGIDev( tracker.bridge.(*GIBridge), dev1, "x", nil )
    } else {
        bridgeDev = NewIIFDev( tracker.bridge.(*IIFBridge), dev1, "x", nil )
    }
    dev := NewDevice( config, tracker, dev1, bridgeDev )
    bridgeDev.SetDevice( dev )
    
    devConfig, hasDevConfig := config.devs[ dev1 ]
    if hasDevConfig {
        bridgeDev.SetConfig( &devConfig )
    }
    
    tracker.DevMap[ dev1 ] = dev
    
    bridgeDev.setProcTracker( tracker )
    
    dev.startBackupVideo()
    
    coroHttpServer( tracker )
    
    return tracker
}

/*func runWDA( cmd *uc.Cmd ) {
    runCleanup( cmd )
    
    id := ""
    idNode := cmd.Get("-id")
    if idNode != nil {
      id = idNode.String()
    }
    
    wda,tracker,_ := wdaForDev( id )
    wda.start( nil )
 
    dotLoop( cmd, tracker )
}*/

func runCFA( cmd *uc.Cmd ) {
    runCleanup( cmd )
    
    id := ""
    idNode := cmd.Get("-id")
    if idNode != nil {
      id = idNode.String()
    }
    
    cfa,tracker,_ := cfaForDev( id )
    cfa.start( nil )
 
    dotLoop( cmd, tracker )
}

func runVidTest( cmd *uc.Cmd ) {
    runCleanup( cmd )
    
    id := ""
    idNode := cmd.Get("-id")
    if idNode != nil {
        id = idNode.String()
    }
    
    tracker := vidTestForDev( id )
    
    dotLoop( cmd, tracker )
}

func dotLoop( cmd *uc.Cmd, tracker *DeviceTracker ) {
    c := make(chan os.Signal)
    stop := make(chan bool)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    go func() {
        <- c
        stop <- true
        tracker.shutdown()
    }()
    
    exit := 0
    for {
        select {
            case <- stop:
                exit = 1
                break
            default:
        }
        if exit == 1 { break }
        fmt.Printf(". ")
        time.Sleep( time.Second * 1 )
    }
    
    runCleanup( cmd )
}

func runWindowSize( cmd *uc.Cmd ) {
    cfaWrapped( cmd, "", func( cfa *CFA ) {
        wid, heg := cfa.WindowSize()
        fmt.Printf("Width: %d, Height: %d\n", wid, heg )
    } )
}

func runAddRec( cmd *uc.Cmd ) {
    cfaWrapped( cmd, "", func( cfa *CFA ) {
        cfa.AddRecordingToCC()
    } )
}

func runAppAt( cmd *uc.Cmd ) {
    cfaWrapped( cmd, "", func( cfa *CFA ) {
        x := 100
        y := 100
        bi := cfa.AppAtPoint(x,y)
        fmt.Printf("bi:%s\n",bi)
    } )
}

func cfaWrapped( cmd *uc.Cmd, appName string, doStuff func( cfa *CFA ) ) {
    config := NewConfig( "config.json", "default.json", "calculated.json" )
  
    runCleanup( cmd )
    
    id := ""
    idNode := cmd.Get("-id")
    if idNode != nil {
        id = idNode.String()
    }
    
    if id == "" {
        tracker := NewDeviceTracker( config, false, []string{} )
        devs := tracker.bridge.GetDevs( config )
        id = devs[0]
    }
    
    cfa,_,dev := cfaForDev( id )
    fmt.Printf("id:[%s]\n", id )
    devConfig := config.devs[ id ]
    fmt.Printf("%+v\n", devConfig )
    
    startChan := make( chan int )
    
    fmt.Printf("devCfaMethod:%s\n", devConfig.cfaMethod )
    var stopChan chan bool
    if config.cfaMethod == "manual" || devConfig.cfaMethod == "manual" {
        fmt.Printf("Manual CFA; connecting...\n")
        go func() {
            cfa.startCfaNng( func( err int, AstopChan chan bool ) {
                stopChan = AstopChan
                fmt.Printf("Manual CFA; connected; err: %d\n", err)
                startChan <- err
            } )
        }()
    } else {
        //cfa.startChan = startChan
        cfa.start( func( err int, AstopChan chan bool ) {
            stopChan = AstopChan
            startChan <- err
        } )
    }
    
    err := <- startChan
    if err != 0 {
        fmt.Printf("Could not start/connect to CFA. Exiting")
        runCleanup( cmd )
        return
    }
    
    fmt.Printf("appName = %s\n", appName)
    if appName == "" {
        fmt.Printf("Ensuring session\n")
        cfa.ensureSession()
        fmt.Printf("Ensured session\n")
    } else {
        cfa.create_session( appName )
    }
    
    doStuff( cfa )
    
    stopChan <- true
    
    dev.shutdown()
    cfa.stop()
    
    runCleanup( cmd )
}

func runClickEl( cmd *uc.Cmd ) {
    cfaWrapped( cmd, "", func( cfa *CFA ) {
        label := cmd.Get("-label").String()
        system := cmd.Get("-system").Bool()
        btnName := cfa.GetEl( "any", label, system, 5 )
        cfa.ElClick( btnName )
    } )
}

func runForceTouchEl( cmd *uc.Cmd ) {
    cfaWrapped( cmd, "", func( cfa *CFA ) {
        label := cmd.Get("-label").String()
        system := cmd.Get("-system").Bool()
        btnName := cfa.GetEl( "any", label, system, 5 )
        cfa.ElForceTouch( btnName, 1 )
    } )
}

func runLongTouchEl( cmd *uc.Cmd ) {
    cfaWrapped( cmd, "", func( cfa *CFA ) {
        label := cmd.Get("-label").String()
        system := cmd.Get("-system").Bool()
        btnName := cfa.GetEl( "any", label, system, 5 )
        cfa.ElLongTouch( btnName )
    } )
}

func runRunApp( cmd *uc.Cmd ) {
    appName := cmd.Get("-name").String()
    cfaWrapped( cmd, appName, func( cfa *CFA ) {
    } )
}

func runSource( cmd *uc.Cmd ) {
    bi := cmd.Get("-bi").String()
    cfaWrapped( cmd, "", func( cfa *CFA ) {
        xml := cfa.Source(bi)
        fmt.Println( xml )
    } )
}

func runAlertInfo( cmd *uc.Cmd ) {
    cfaWrapped( cmd, "", func( cfa *CFA ) {
        _, json := cfa.AlertInfo()
        fmt.Println( json )
    } )
}

func runIsLocked( cmd *uc.Cmd ) {
    cfaWrapped( cmd, "", func( cfa *CFA ) {
        locked := cfa.IsLocked()
        if locked {
            fmt.Println("Device screen is locked")
        } else {
            fmt.Println("Device screen is unlocked")
        }
    } )
}

func runUnlock( cmd *uc.Cmd ) {
    cfaWrapped( cmd, "", func( cfa *CFA ) {
        //cfa.Unlock()
        cfa.ioHid( 0x0c, 0x30 ) // power
        //time.Sleep(time.Second)
        //cfa.ioHid( 0x07, 0x4a ) // home keyboard button
        cfa.Unlock()
    } )
}

func runListen( cmd *uc.Cmd ) {
    stopChan := make( chan bool )
    listenForDevices( stopChan,
        func( id string, goIosDevice ios.DeviceEntry ) {
            fmt.Printf("Connected %s\n", id )
        },
        func( id string ) {
            fmt.Printf("Disconnected %s\n", id )
        } )
    
    c := make(chan os.Signal, syscall.SIGTERM)
    signal.Notify(c, os.Interrupt)
    <-c
}

func common( cmd *uc.Cmd ) *Config {
    debug := cmd.Get("-debug").Bool()
    warn  := cmd.Get("-warn").Bool()
    
    configPath := cmd.Get("-config").String()
    if configPath == "" { configPath = "config.json" }
    
    defaultsPath := cmd.Get("-defaults").String()
    if defaultsPath == "" { defaultsPath = "default.json" }
    
    calculatedPath := cmd.Get("-calculated").String()
    if calculatedPath == "" { calculatedPath = "calculated.json" }
    
    setupLog( debug, warn )
    
    config := NewConfig( configPath, defaultsPath, calculatedPath )
    config.cpuProfile = cmd.Get("-cpuprofile").Bool()
    return config
}

func runCleanup( *uc.Cmd ) {
    config := NewConfig( "config.json", "default.json", "calculated.json" )
    cleanup_procs( config )    
}

func runRegister( cmd *uc.Cmd ) {
    config := common( cmd )
    
    doregister( config )
}

func runMain( cmd *uc.Cmd ) {
    config := common( cmd )
    
    // This seems to do nothing... what gives
    /*if config.cpuProfile {
        f, _ := os.Create("cpuprofile")
        if err == nil {
            pprof.StartCPUProfile( f )
            defer pprof.StopCPUProfile()
        }
    }*/
    
    idNode := cmd.Get("-id")
    ids := []string{}
    if idNode != nil {
        idString := idNode.String()
        if idString != "" {
            ids = strings.Split( idString, "," )
            config.idList = ids
        }
    }
    
    cleanup_procs( config )
    
    nosanity := cmd.Get("-nosanity").Bool()
    if !nosanity {
        sane := sanityChecks( config, cmd )
        if !sane {
            fmt.Printf("Sanity checks failed. Exiting\n")
            return
        }
    }
    
    devTracker := NewDeviceTracker( config, true, ids )
    coro_sigterm( config, devTracker )
    
    coroHttpServer( devTracker )
}

func setupLog( debug bool, warn bool ) {
    //log.SetFormatter(&log.JSONFormatter{})
    log.SetFormatter(&log.TextFormatter{
        DisableTimestamp: true,
    })
    log.SetOutput(os.Stdout)
    if debug {
        log.SetLevel( log.DebugLevel )
    } else if warn {
        log.SetLevel( log.WarnLevel )
    } else {
        log.SetLevel( log.InfoLevel )
    }
}

func censorUuid( uuid string ) (string) {
    return "***" + uuid[len(uuid)-4:]
}