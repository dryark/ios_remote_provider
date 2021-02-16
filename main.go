package main

import (
    "flag"
    "os"
    //"sync"
    log "github.com/sirupsen/logrus"
)

func main() {
    var debug      = flag.Bool(   "debug" , false        , "Use debug log level" )
    var warn       = flag.Bool(   "warn"  , false        , "Use warn log level" )
    var configPath = flag.String( "config", "config.json", "Config file path" )
    var defaultsPath = flag.String( "defaults", "default.json", "Defaults file path" )
    var register   = flag.Bool(   "register", false      , "Register against control floor" )
    flag.Parse()
    
    setupLog( *debug, *warn )
    
    config := NewConfig( *configPath, *defaultsPath )
    
    if *register {
        doregister( config )
        return
    }
    
    cleanup_procs( config )
        
    devTracker := NewDeviceTracker( config )
    coro_sigterm( config, devTracker )
    
    coroHttpServer( devTracker )
    
    // The devTracker now handles this directly
    // proc_device_trigger( devTracker )
    
    //devTracker.eventLoop()
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