package main

import (
    "os"
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
    uclop.AddCmd( "run", "Run ControlFloor", runMain, commonOpts )
    uclop.AddCmd( "register", "Register against ControlFloor", runRegister, commonOpts )
    uclop.AddCmd( "cleanup", "Cleanup leftover processes", runCleanup, nil )
    uclop.Run()
}

func common( cmd *uc.Cmd ) *Config {
    debug := cmd.Get("-debug").Bool()
    warn  := cmd.Get("-warn").Bool()
    
    configPath := cmd.Get("-config").String()
    if configPath == "" { configPath = "config.json" }
    
    defaultsPath := cmd.Get("-defaults").String()
    if defaultsPath == "" { defaultsPath = "defauls.json" }
    
    setupLog( debug, warn )
    
    return NewConfig( configPath, defaultsPath )
}

func runCleanup( *uc.Cmd ) {
    config := NewConfig( "config.json", "default.json" )
    cleanup_procs( config )
    
    /*out, _ := exec.Command( "ps", "-eo", "pid,args" ).Output()
    lines := strings.Split( string(out), "\n" )
    for _,line := range lines {
        if line == "" { continue }
        
        i := 0
        for ; i<len(line) ; i++ {
            if line[i] != ' ' { break }
        }
        line := line[i:]
        
        parts := strings.Split( line, " " )
        if strings.Contains( parts[1], "iosif" ) {
            fmt.Printf("line:%s %s\n", parts[0], parts[1] )
        }
    }*/
    
    //fmt.Println( string(out) )
}

func runRegister( cmd *uc.Cmd ) {
    config := common( cmd )
    
    doregister( config )
}

func runMain( cmd *uc.Cmd ) {
    config := common( cmd )
        
    cleanup_procs( config )
        
    devTracker := NewDeviceTracker( config )
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