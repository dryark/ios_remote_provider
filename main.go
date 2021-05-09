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
    uclop.AddCmd( "wda", "Just run WDA", runWDA, nil )
    
    uclop.Run()
}

func wdaForDev1() (*WDA,*DeviceTracker) {
    config := NewConfig( "config.json", "default.json" )
    
    devs := GetDevs( config )
    dev1 := devs[0]
    fmt.Printf("Dev id: %s\n", dev1)
    
    tracker := NewDeviceTracker( config, false )
    iifDev := NewIIFDev( tracker.bridge.(*IIFBridge), dev1, "x" )
    dev := NewDevice( config, tracker, dev1, iifDev )
    iifDev.setProcTracker( tracker )
    wda := NewWDA( config, tracker, dev, 8100 )
    return wda,tracker
}

func runWDA( cmd *uc.Cmd ) {
    runCleanup( cmd )
    
    _,tracker := wdaForDev1()
 
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
    
    wda,tracker := wdaForDev1()
    startChan := make( chan bool )
    wda.startChan = startChan
    <- startChan
    
    wda.ensureSession()
    wda.OpenControlCenter("bottomUp")
    recBtn := wda.ElByName( "Screen Recording" )
    fmt.Printf("recBtn:%s\n", recBtn )
    wda.ElForceTouch( recBtn, 2000 )
    
    dotLoop( cmd, tracker )
}

func common( cmd *uc.Cmd ) *Config {
    debug := cmd.Get("-debug").Bool()
    warn  := cmd.Get("-warn").Bool()
    
    configPath := cmd.Get("-config").String()
    if configPath == "" { configPath = "config.json" }
    
    defaultsPath := cmd.Get("-defaults").String()
    if defaultsPath == "" { defaultsPath = "default.json" }
    
    setupLog( debug, warn )
    
    return NewConfig( configPath, defaultsPath )
}

func runCleanup( *uc.Cmd ) {
    config := NewConfig( "config.json", "default.json" )
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