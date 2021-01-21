package main

import (
    "flag"
    "os"
    "sync"
    log "github.com/sirupsen/logrus"
)

func NewDevice( config *Config, devTracker *DeviceTracker, uuid string ) (*Device) {
    dev := Device{
        devTracker: devTracker,
        wdaPort:    devTracker.getPort(),
        vidPort:   devTracker.getPort(),
        vidControlPort:   devTracker.getPort(),
        config:     config,
        uuid:       uuid,
        lock:       &sync.Mutex{},
        process:    make( map[string] *GenericProc ),
        cf:         devTracker.cf,
        EventCh:    make( chan DevEvent ),
    }
    return &dev
}

func main() {
    var debug      = flag.Bool(   "debug" , false        , "Use debug log level" )
    var warn       = flag.Bool(   "warn"  , false        , "Use warn log level" )
    var configPath = flag.String( "config", "config.json", "Config file path" )
    var register   = flag.Bool(   "register", false      , "Register against control floor" )
    flag.Parse()
    
    setupLog( *debug, *warn )
    
    config := NewConfig( *configPath )
    
    if *register {
        doregister( config )
        return
    }
    
    cleanup_procs( config )
        
    devTracker := NewDeviceTracker( config )
    coro_sigterm( config, devTracker )
    
    coroHttpServer( devTracker )
    
    proc_device_trigger( devTracker )
    
    devTracker.eventLoop()
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