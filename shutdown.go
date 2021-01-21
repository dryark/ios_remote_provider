package main

import (
	"fmt"
    "os"
    "os/signal"
    "syscall"
    "time"
    log "github.com/sirupsen/logrus"
    si "github.com/elastic/go-sysinfo"
)

func coro_sigterm( config *Config, devTracker *DeviceTracker ) {
    c := make(chan os.Signal, 2)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    go func() {
        <- c
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
    }()
}

func cleanup_procs( config *Config ) {
    plog := log.WithFields( log.Fields{
        "type": "proc_cleanup",
    } )

    procMap := map[string]string {
        "ios_video_stream": config.iosVideoStreamPath,
        "ios-deploy":       config.iosDeployPath,
        "xcodebuild": "/Applications/Xcode.app/Contents/Developer/usr/bin/xcodebuild",
        "mobiledevice": "/Users/user/git/ios_remote_provider/bin/mobiledevice",
        "mobiledevice2": "bin/mobiledevice",
    }
     
	// Cleanup hanging processes if any
    procs, listErr := si.Processes()
    if listErr != nil {
    	fmt.Printf( "listErr:%s\n", listErr )
    	os.Exit(1)
    }
    
    var hangingPids []int
    
    for _, proc := range procs {
    	info, infoErr := proc.Info()
    	if infoErr != nil { continue }
    	
        cmd := info.Args
        
        for k,v := range procMap {
            if cmd[0] == v {
                pid := proc.PID()
                plog.WithFields( log.Fields{
                    "proc": k,
                    "pid":  pid,
                } ).Warn("Leftover " + k + " - Sending SIGTERM")
                
                syscall.Kill( pid, syscall.SIGTERM )
                hangingPids = append( hangingPids, pid ) 
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