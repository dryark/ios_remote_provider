package main

import (
    "fmt"
    "os"
    "os/signal"
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
    
    runOpts := append( commonOpts,
        uc.OPT("-nosanity","Skip sanity checks",uc.FLAG),
    )
    
    uclop.AddCmd( "run", "Run ControlFloor", runMain, runOpts )
    uclop.AddCmd( "register", "Register against ControlFloor", runRegister, commonOpts )
    uclop.AddCmd( "cleanup", "Cleanup leftover processes", runCleanup, nil )
    
    clickOpts := uc.OPTS{
        uc.OPT("-el","Element name to click",0),
    }
    uclop.AddCmd( "click", "Click element", runClick, clickOpts )
    
    wdaOpts := uc.OPTS{
        uc.OPT("-id","Udid of device",0),
    }
    uclop.AddCmd( "wda", "Just run WDA", runWDA, wdaOpts )
    
    windowSizeOpts := uc.OPTS{
        uc.OPT("-id","Udid of device",0),
    }
    uclop.AddCmd( "winsize", "Get device window size", runWindowSize, windowSizeOpts )
    uclop.AddCmd( "source", "Get device xml source", runSource, windowSizeOpts )
    
    clickButtonOpts := uc.OPTS{
        uc.OPT("-id","Udid of device",0),
        uc.OPT("-label","Button label",uc.REQ),
    }
    uclop.AddCmd( "clickEl", "Click a named element", runClickEl, clickButtonOpts )
    
    vidTestOpts := uc.OPTS{
        uc.OPT("-id","Udid of device",0),
    }
    uclop.AddCmd( "vidtest", "Test backup video", runVidTest, vidTestOpts ) 
    
    uclop.Run()
}

func wdaForDev( id string ) (*WDA,*DeviceTracker) {
    config := NewConfig( "config.json", "default.json", "calculated.json" )
    
    devs := GetDevs( config )
    dev1 := id
    if id == "" {
        dev1 = devs[0]
    }
    fmt.Printf("Dev id: %s\n", dev1)
    
    tracker := NewDeviceTracker( config, false )
    iifDev := NewIIFDev( tracker.bridge.(*IIFBridge), dev1, "x" )
    dev := NewDevice( config, tracker, dev1, iifDev )
    iifDev.setProcTracker( tracker )
    dev.wdaPort = 8100
    wda := NewWDANoStart( config, tracker, dev )
    return wda,tracker
}

func vidTestForDev( id string ) (*DeviceTracker) {
    config := NewConfig( "config.json", "default.json", "calculated.json" )
    
    devs := GetDevs( config )
    dev1 := id
    if id == "" {
        dev1 = devs[0]
    }
    fmt.Printf("Dev id: %s\n", dev1)
    
    tracker := NewDeviceTracker( config, false )
    iifDev := NewIIFDev( tracker.bridge.(*IIFBridge), dev1, "x" )
    dev := NewDevice( config, tracker, dev1, iifDev )
    tracker.DevMap[ dev1 ] = dev
    
    iifDev.setProcTracker( tracker )
    
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
    
    wda,tracker := wdaForDev( id )
    wda.start()
 
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



func runClick( cmd *uc.Cmd ) {
    runCleanup( cmd )
    
    wda,tracker := wdaForDev("")
    startChan := make( chan int )
    wda.startChan = startChan
    wda.start()
    
    <- startChan
    
    wda.ensureSession()
    // todo
    
    dotLoop( cmd, tracker )
}

func runWindowSize( cmd *uc.Cmd ) {
    config := NewConfig( "config.json", "default.json", "calculated.json" )
  
    runCleanup( cmd )
    
    id := ""
    idNode := cmd.Get("-id")
    if idNode != nil {
        id = idNode.String()
    }
    
    wda,_ := wdaForDev( id )
    
    startChan := make( chan int )
    
    if config.wdaMethod == "manual" {
        wda.startWdaNng( func( err int ) {
            startChan <- err
        } )
    } else {
        wda.startChan = startChan
        wda.start()
    }
    
    err := <- startChan
    if err != 0 {
        fmt.Printf("Could not start/connect to WDA. Exiting")
        runCleanup( cmd )
        return
    }
    
    fmt.Printf("WDA supposedly started")
    
    wda.ensureSession()
    //wda.create_session("")
    wid, heg := wda.WindowSize()
    fmt.Printf("Width: %d, Height: %d\n", wid, heg )
    wda.stop()
    
    runCleanup( cmd )
}

func runClickEl( cmd *uc.Cmd ) {
    config := NewConfig( "config.json", "default.json", "calculated.json" )
  
    runCleanup( cmd )
    
    id := ""
    idNode := cmd.Get("-id")
    if idNode != nil {
        id = idNode.String()
    }
    
    wda,_ := wdaForDev( id )
    
    startChan := make( chan int )
    
    if config.wdaMethod == "manual" {
        wda.startWdaNng( func( err int ) {
            startChan <- err
        } )
    } else {
        wda.startChan = startChan
        wda.start()
    }
    
    err := <- startChan
    if err != 0 {
        fmt.Printf("Could not start/connect to WDA. Exiting")
        runCleanup( cmd )
        return
    }
    
    wda.ensureSession()
    //wda.create_session("")
    
    label := cmd.Get("-label").String()
    btnName := wda.ElByName( label )
    wda.ElClick( btnName )
    
    wda.stop()
    
    runCleanup( cmd )
}

func runSource( cmd *uc.Cmd ) {
    config := NewConfig( "config.json", "default.json", "calculated.json" )
  
    runCleanup( cmd )
    
    id := ""
    idNode := cmd.Get("-id")
    if idNode != nil {
        id = idNode.String()
    }
    
    wda,_ := wdaForDev( id )
    
    startChan := make( chan int )
    
    if config.wdaMethod == "manual" {
        wda.startWdaNng( func( err int ) {
            startChan <- err
        } )
    } else {
        wda.startChan = startChan
        wda.start()
    }
    err := <- startChan
    if err != 0 {
        fmt.Printf("Error starting/connecting to WDA\n")
        runCleanup( cmd )
        return
    }
    fmt.Printf("WDA supposedly started")
    
    //wda.ensureSession()
    wda.create_session("")
    xml := wda.Source()
    fmt.Println( xml )
    wda.stop()
    
    runCleanup( cmd )
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
        
    cleanup_procs( config )
    
    nosanity := cmd.Get("-nosanity").Bool()
    if !nosanity {
        sane := sanityChecks( config, cmd )
        if !sane { return }
    }
    
    devTracker := NewDeviceTracker( config, true )
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