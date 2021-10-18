package main

import (
    "fmt"
    "os"
    "os/exec"
    "os/signal"
    "syscall"
    "strings"
    "strconv"
    "time"
    log "github.com/sirupsen/logrus"
    si "github.com/elastic/go-sysinfo"
)

func coro_sigterm( config *Config, devTracker *DeviceTracker ) {
    c := make(chan os.Signal, 2)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    go func() {
        <- c
        do_shutdown( config, devTracker )
    }()
}

func do_shutdown( config *Config, devTracker *DeviceTracker ) {
    log.WithFields( log.Fields{
        "type":  "sigterm",
        "state": "begun",
    } ).Info("Shutdown started")

    devTracker.shutdown()
    
    log.WithFields( log.Fields{
        "type":  "sigterm",
        "state": "stopped",
    } ).Info("Normal stop done... cleaning up leftover procs")

    cleanup_procs( config )
    
    log.WithFields( log.Fields{
        "type":  "sigterm",
        "state": "done",
    } ).Info("Shutdown finished")
    
    os.Exit(0)
}

type Aproc struct {
    pid int
    cmd string
    args []string
}

func get_procs() []Aproc {
    out, _ := exec.Command( "ps", "-eo", "pid,args" ).Output()
    lines := strings.Split( string(out), "\n" )
    
    procs := []Aproc{}
    for _,line := range lines {
        if line == "" { continue }
        
        i := 0
        for ; i<len(line) ; i++ {
            if line[i] != ' ' { break }
        }
        line := line[i:]
        
        parts := strings.Split( line, " " )
        //if strings.Contains( parts[1], "iosif" ) {
        //    fmt.Printf("line:%s %s\n", parts[0], parts[1] )
        //}
        pid, _ := strconv.Atoi( parts[0] )
        
        args := []string{}
        if len( parts ) > 2 {
            args = parts[2:]
        }
        procs = append( procs, Aproc{ pid: pid, cmd: parts[1], args: args } )
    }
    return procs
}

func cleanup_procs( config *Config ) {
    plog := log.WithFields( log.Fields{
        "type": "proc_cleanup",
    } )

    procMap := map[string]string {
        "iosif":       config.iosIfPath,
        "xcodebuild": "/Applications/Xcode.app/Contents/Developer/usr/bin/xcodebuild",
    }
     
	  procs := get_procs()
    
    var hangingPids []int
    
    for _,proc := range procs {
        for k,v := range procMap {
            if proc.cmd == v {
                pid := proc.pid //proc.PID()
                plog.WithFields( log.Fields{
                    "proc": k,
                    "pid":  pid,
                } ).Warn("Leftover " + k + " - Sending SIGTERM")
                
                syscall.Kill( pid, syscall.SIGTERM )
                hangingPids = append( hangingPids, pid ) 
            }
        }
    }
    
    if len( config.idList ) > 0 {
        fmt.Printf("Running in singleId mode; killing procs with id %s\n", strings.Join( config.idList, "," ) )
    }
    
    // Death to all tidevice processes! *rage*
    for _,proc := range procs {
        if strings.Contains( proc.cmd, "tidevice" ) ||
            (
                strings.HasSuffix( proc.cmd, "Python" ) &&
                proc.args[0] == "-m" &&
                proc.args[1] == "tidevice" ) ||
            (
                strings.HasSuffix( proc.cmd, "Python" ) &&
                strings.HasSuffix( proc.args[0], "tidevice" ) ) {
            pid := proc.pid //proc.PID()
            plog.WithFields( log.Fields{
                "proc": "tidevice",
                "pid":  pid,
            } ).Warn("Leftover tidevice - Sending SIGTERM")
            
            syscall.Kill( pid, syscall.SIGTERM )
            hangingPids = append( hangingPids, pid ) 
        }
        if strings.Contains( proc.cmd, "go-ios" ) {
            pid := proc.pid //proc.PID()
            doKill := false        
            /*
            If using a singleId ( udid specified on CLI ), then we don't want to get rid of all
            go-ios procs, only the ones associated with that ID.
            */
            if len( config.idList ) > 0 {
                for _, arg := range( proc.args ) {
                    for _, oneId := range( config.idList ) {
                        if strings.Contains( arg, oneId ) {
                            doKill = true
                        }
                    }
                }
            } else {
                doKill = true
            }
            
            if doKill == true {
                syscall.Kill( pid, syscall.SIGTERM )
                hangingPids = append( hangingPids, pid ) 
           
                plog.WithFields( log.Fields{
                    "proc": "go-ios",
                    "pid":  pid,
                    "args": proc.args,
                } ).Warn("Leftover go-ios - Sending SIGTERM")
            }
        }
        if strings.Contains( proc.cmd, "iosif" ) {
            pid := proc.pid //proc.PID()
            doKill := false        
            /*
            If using a singleId ( udid specified on CLI ), then we don't want to get rid of all
            iosif procs, only the ones associated with that ID.
            */
            if len( config.idList ) > 0 {
                for _, arg := range( proc.args ) {
                    for _, oneId := range( config.idList ) {
                        if strings.Contains( arg, oneId ) {
                            doKill = true
                        }
                    }
                }
            } else {
                doKill = true
            }
            
            if doKill == true {
                syscall.Kill( pid, syscall.SIGTERM )
                hangingPids = append( hangingPids, pid ) 
           
                plog.WithFields( log.Fields{
                    "proc": "iosif",
                    "pid":  pid,
                    "args": proc.args,
                } ).Warn("Leftover iosif - Sending SIGTERM")
            }
        }
    }
    
    if len( hangingPids ) > 0 {
        // Give the processes half a second to shudown cleanly
        time.Sleep( time.Millisecond * 500 )
        
        // Send kill to processes still around
        for _, pid := range( hangingPids ) {
            proc, _ := si.Process( pid )
            if proc != nil {
                info, infoErr := proc.Info()
                arg0 := "unknown"
                if infoErr == nil {
                    args := info.Args
                    arg0 = args[0]
                } else {
                    // If the process vanished before here; it errors out fetching info
                    continue
                }
                
                plog.WithFields( log.Fields{
                    "arg0": arg0,
                } ).Warn("Leftover Proc - Sending SIGKILL")
                syscall.Kill( pid, syscall.SIGKILL )
            }
        }
    
        // Spend up to 500 ms waiting for killed processes to vanish
        i := 0
        for {
            i = i + 1
            time.Sleep( time.Millisecond * 100 )
            allGone := 1
            for _, pid := range( hangingPids ) {
                proc, _ := si.Process( pid )
                if proc != nil {
                    _, infoErr := proc.Info()
                    if infoErr != nil {
                        continue
                    }
                    allGone = 0
                }
            }
            if allGone == 1 && i > 5 {
                break
            }
        }
        
        // Write out error messages for processes that could not be killed
        for _, pid := range( hangingPids ) {
            proc, _ := si.Process( pid )
            if proc != nil {
                info, infoErr := proc.Info()
                arg0 := "unknown"
                if infoErr != nil {
                    continue
                }
                args := info.Args
                arg0 = args[0]
                
                plog.WithFields( log.Fields{
                    "arg0": arg0,
                } ).Error("Kill attempted and failed")
            }
        }
    }
}