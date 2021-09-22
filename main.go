package main

import (
    "fmt"
    "os"
    "os/signal"
    "strings"
    "syscall"
    "time"
    log "github.com/sirupsen/logrus"
    uc "github.com/nanoscopic/uclop/mod"
)

func main() {
    uclop := uc.NewUclop()
    commonOpts := uc.OPTS{
        uc.OPT("-debug","Use debug log level",uc.FLAG),
        uc.OPT("-warn","Use warn log level",uc.FLAG),
        uc.OPT("-config","Config file to use",0),
        uc.OPT("-defaults","Defaults config file to use",0),
        uc.OPT("-calculated","Path to calculated JSON values",0),
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

    uclop.AddCmd( "wda",       "Just run WDA",                     runWDA,        idOpt )
    uclop.AddCmd( "winsize",   "Get device window size",           runWindowSize, idOpt )
    uclop.AddCmd( "source",    "Get device xml source",            runSource,     idOpt )
    uclop.AddCmd( "alertinfo", "Get alert info",                   runAlertInfo,  idOpt )
    uclop.AddCmd( "islocked",  "Check if device screen is locked", runIsLocked,   idOpt )
    uclop.AddCmd( "unlock",    "Unlock device screen",             runUnlock,     idOpt )
    uclop.AddCmd( "listen",    "Test listening for devices",       runListen,     commonOpts )
    
    clickButtonOpts := append( idOpt,
        uc.OPT("-label","Button label",uc.REQ),
    )
    uclop.AddCmd( "clickEl", "Click a named element", runClickEl, clickButtonOpts )
    
    runAppOpts := append( idOpt,
        uc.OPT("-name","App name",uc.REQ),
    )
    uclop.AddCmd( "runapp", "Run named app", runRunApp, runAppOpts )
        
    uclop.AddCmd( "vidtest", "Test backup video", runVidTest, idOpt ) 
    
    uclop.Run()
}

func wdaForDev( id string ) (*WDA,*DeviceTracker,*Device) {
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
        bridgeDev = NewGIDev( tracker.bridge.(*GIBridge), dev1, "x" )
    } else {
        bridgeDev = NewIIFDev( tracker.bridge.(*IIFBridge), dev1, "x" )
    }
    
    devConfig, hasDevConfig := config.devs[ dev1 ]
    if hasDevConfig {
        bridgeDev.SetConfig( &devConfig )
    }
    
    dev := NewDevice( config, tracker, dev1, bridgeDev )
    bridgeDev.setProcTracker( tracker )
    dev.wdaPort = 8100
    wda := NewWDANoStart( config, tracker, dev )
    return wda,tracker,dev
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
        bridgeDev = NewGIDev( tracker.bridge.(*GIBridge), dev1, "x" )
    } else {
        bridgeDev = NewIIFDev( tracker.bridge.(*IIFBridge), dev1, "x" )
    }
    
    devConfig, hasDevConfig := config.devs[ dev1 ]
    if hasDevConfig {
        bridgeDev.SetConfig( &devConfig )
    }
    
    dev := NewDevice( config, tracker, dev1, bridgeDev )
    
    tracker.DevMap[ dev1 ] = dev
    
    bridgeDev.setProcTracker( tracker )
    
    dev.startBackupVideo()
    
    coroHttpServer( tracker )
    
    return tracker
}

func runWDA( cmd *uc.Cmd ) {
    runCleanup( cmd )
    
    id := ""
    idNode := cmd.Get("-id")
    if idNode != nil {
      id = idNode.String()
    }
    
    wda,tracker,_ := wdaForDev( id )
    wda.start( nil )
 
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
    wdaWrapped( cmd, "", func( wda *WDA ) {
      wid, heg := wda.WindowSize()
        fmt.Printf("Width: %d, Height: %d\n", wid, heg )
    } )
}

func wdaWrapped( cmd *uc.Cmd, appName string, doStuff func( wda *WDA ) ) {
    config := NewConfig( "config.json", "default.json", "calculated.json" )
  
    runCleanup( cmd )
    
    id := ""
    idNode := cmd.Get("-id")
    if idNode != nil {
        id = idNode.String()
    }
    
    wda,_,dev := wdaForDev( id )
    
    startChan := make( chan int )
    
    var stopChan chan bool
    if config.wdaMethod == "manual" {
        wda.startWdaNng( func( err int, AstopChan chan bool ) {
            stopChan = AstopChan
            startChan <- err
        } )                             
    } else {
        //wda.startChan = startChan
        wda.start( func( err int, AstopChan chan bool ) {
            stopChan = AstopChan
            startChan <- err
        } )
    }
    
    err := <- startChan
    if err != 0 {
        fmt.Printf("Could not start/connect to WDA. Exiting")
        runCleanup( cmd )
        return
    }
    
    if appName == "" {
        wda.ensureSession()
    } else {
        sid := wda.create_session( appName )
        wda.sessionId = sid
    }
    
    doStuff( wda )
    
    stopChan <- true
    
    dev.shutdown()
    wda.stop()
    
    runCleanup( cmd )
}

func runClickEl( cmd *uc.Cmd ) {
    wdaWrapped( cmd, "", func( wda *WDA ) {
        label := cmd.Get("-label").String()
        btnName := wda.ElByName( label )
        wda.ElClick( btnName )
    } )
}

func runRunApp( cmd *uc.Cmd ) {
    appName := cmd.Get("-name").String()
    wdaWrapped( cmd, appName, func( wda *WDA ) {
    } )
}

func runSource( cmd *uc.Cmd ) {
    wdaWrapped( cmd, "", func( wda *WDA ) {
        xml := wda.Source()
        fmt.Println( xml )
    } )
}

func runAlertInfo( cmd *uc.Cmd ) {
    wdaWrapped( cmd, "", func( wda *WDA ) {
        _, json := wda.AlertInfo()
        fmt.Println( json )
    } )
}

func runIsLocked( cmd *uc.Cmd ) {
    wdaWrapped( cmd, "", func( wda *WDA ) {
        locked := wda.IsLocked()
        if locked {
            fmt.Println("Device screen is locked")
        } else {
            fmt.Println("Device screen is unlocked")
        }
    } )
}

func runUnlock( cmd *uc.Cmd ) {
    wdaWrapped( cmd, "", func( wda *WDA ) {
        //wda.Unlock()
        wda.ioHid( 0x0c, 0x30 ) // power
        //time.Sleep(time.Second)
        //wda.ioHid( 0x07, 0x4a ) // home keyboard button
        wda.Unlock()
    } )
}

func runListen( cmd *uc.Cmd ) {
    stopChan := make( chan bool )
    listenForDevices( stopChan,
        func( id string ) {
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
    
    return NewConfig( configPath, defaultsPath, calculatedPath )
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
    
    idNode := cmd.Get("-id")
    ids := []string{}
    if idNode != nil {
        idString := idNode.String()
        ids = strings.Split( idString, "," )
        config.idList = ids
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