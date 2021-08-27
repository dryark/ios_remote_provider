package main

import (
    "fmt"
    log "github.com/sirupsen/logrus"
    gocmd "github.com/go-cmd/cmd"
    "os"
    "time"
)

type OutputHandler func( string, *log.Entry )

type ProcOptions struct {
    dev           *Device
    procName      string
    binary        string
    args          []string
    stderrHandler OutputHandler
    stdoutHandler OutputHandler
    startFields   log.Fields
    startDir      string
    env           map[string]string
    noRestart     bool
    noWait        bool
    onStop        func( interface{} )
}

type ProcTracker interface {
    startProc( proc *GenericProc )
    stopProc( procName string )
    isShuttingDown() bool
}

type GPMsg struct {
    msgType int
}

type GenericProc struct {
    name      string
    controlCh chan GPMsg
    backoff   *Backoff
    pid       int
    cmd       *gocmd.Cmd
}

func (self *GenericProc) Kill() {
    if self.cmd == nil { return }
    self.controlCh <- GPMsg{ msgType: 1 }
}

func (self *GenericProc) Restart() {
    if self.cmd == nil { return }
    self.controlCh <- GPMsg{ msgType: 2 }
}

func restart_proc_generic( dev *Device, name string ) {
    genProc := dev.process[ name ]
    genProc.Restart()
}

func proc_generic( procTracker ProcTracker, wrapper interface{}, opt *ProcOptions ) ( *GenericProc ) {
    controlCh := make( chan GPMsg )
    proc := GenericProc {
        controlCh: controlCh,
        name: opt.procName,
    }
        
    var plog *log.Entry

    plog = log.WithFields( log.Fields{ "proc": opt.procName } )
    
    if procTracker != nil {
        procTracker.startProc( &proc )
    } else {
        panic("procTracker not set")
    }
  
    backoff := Backoff{}
    proc.backoff = &backoff

    stop := false
    
    if opt.binary == "" {
        fmt.Printf("Binary not set\n")
    }
    
    _, ferr := os.Stat( opt.binary )
    if ferr != nil {
        plog.WithFields( log.Fields{
            "type":  "proc_bin_missing",
            "error": ferr,
            "path":  opt.binary,
        } ).Fatal("Binary path does not exist. Cannot start process")
    }
    
    startFields := log.Fields{
        "type":   "proc_start",
        "binary": opt.binary,
    }
    if opt.startFields != nil {
        for k, v := range opt.startFields {
            if v == nil {
                fmt.Printf("%s not set\n", k )
            }
            //fmt.Printf("%s = %s\n", k, v )
            startFields[k] = v
        }
    }
    
    plog.WithFields( log.Fields{
        "type":  "proc_fields",
        "fields": startFields,
    } ).Debug("Process starting fields")
    
    go func() { for {
        plog.WithFields( startFields ).Info("Process start - " + opt.procName)

        cmd := gocmd.NewCmdOptions( gocmd.Options{ Buffered: false, Streaming: true }, opt.binary, opt.args... )
        proc.cmd = cmd
        
        if opt.startDir != "" {
            cmd.Dir = opt.startDir
        }
        
        if opt.env != nil {
            var envArr []string
            for k,v := range( opt.env ) {
                envArr = append( envArr, fmt.Sprintf("%s=%s", k, v ) )
            }
            cmd.Env = envArr
        }

        backoff.markStart()
        
        statCh := cmd.Start()
        
        i := 0
        for {
            status := cmd.Status()
            
            if status.Error != nil {
                errStream := cmd.Stderr
                
                errText := ""
                for {
                  select {
                    case line, _ := <- errStream:
                      errText = errText + line
                    default:
                      break
                  }
                }
                
                plog.WithFields( log.Fields{
                    "type":  "proc_err",
                    "error": status.Error,
                    "text": errText,
                } ).Error("Error starting - " + opt.procName)
                
                return
            }
            
            if status.Exit != -1 {
                errStream := cmd.Stderr
                errText := ""
                for {
                  select {
                    case line, _ := <- errStream:
                      errText = errText + line
                    default:
                      break
                  }
                }
                
                plog.WithFields( log.Fields{
                    "type": "proc_exit",
                    "exit": status.Exit,
                    "args": opt.args,
                    "text": errText,
                } ).Error("Error starting - " + opt.procName)
                
                return
            }
            
            proc.pid = status.PID
            if proc.pid != 0 {
                break
            }
            time.Sleep(50 * time.Millisecond)
            if i > 4 {
                break
            }
        }
                
        plog.WithFields( log.Fields{
            "type": "proc_pid",
            "pid":  proc.pid,
        } ).Debug("Process pid")
        
        outStream := cmd.Stdout
        errStream := cmd.Stderr
        
        runDone := false
        for {
            select {
                case <- statCh:
                    runDone = true
                case msg := <- controlCh:
                    plog.Debug("Got stop request on control channel")
                    if msg.msgType == 1 { // stop
                        stop = true
                        proc.cmd.Stop()
                    } else if msg.msgType == 2 { // restart
                        proc.cmd.Stop()
                    }
                case line, _ := <- outStream:
                    if line == "" { continue }
                    if opt.stdoutHandler != nil {
                        opt.stdoutHandler( line, plog )
                    } else {
                        plog.WithFields( log.Fields{ "line": line } ).Info("")
                    }
                case line, _ := <- errStream:
                    if opt.stderrHandler != nil {
                        opt.stderrHandler( line, plog )
                    } else {
                        if line != "" {
                            plog.WithFields( log.Fields{ "line": line, "iserr": true } ).Info("")
                        }
                    }
            }
            if runDone { break }
        }
        
        proc.cmd = nil
        
        backoff.markEnd()

        plog.WithFields( log.Fields{ "type": "proc_end" } ).Warn("Process end - "+ opt.procName)
        
        if opt.onStop != nil {
            opt.onStop( wrapper )
        }
        
        if opt.noRestart { 
            plog.Debug( "No restart requested" )
            break
        }
        
        if stop { break }
        
        if !opt.noWait {
            backoff.wait()
        } else {
            plog.Debug("No wait requested")
        }
        
        if procTracker.isShuttingDown() { break }
    } }()
    
    return &proc
}