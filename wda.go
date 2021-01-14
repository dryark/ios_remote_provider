package main

import (
    "fmt"
    "os"
    "os/exec"
    "strings"
    "path/filepath"
    "regexp"
    "strconv"
    log "github.com/sirupsen/logrus"
)

type WDA struct {
    uuid         string
    onDevicePort int
    localhostPort int
    devTracker   *DeviceTracker
    dev          *Device
    wdaProc      *GenericProc
    config       *Config
}

func NewWDA( config *Config, devTracker *DeviceTracker, dev *Device, localhostPort int ) (*WDA) {
    self := WDA{
        uuid: dev.uuid,
        onDevicePort: 8100,
        localhostPort: localhostPort,
        devTracker: devTracker,
        dev: dev,
        config: config,
    }
    
    self.start()
    
    return &self
}

func (self *WDA) start() {
    go func() {
        spec := fmt.Sprintf("%d:%d", self.localhostPort, self.onDevicePort )
        
        log.WithFields( log.Fields{
            "bin": self.config.mobiledevicePath,
            "uuid": censorUuid( self.uuid ),
            "spec": spec,
        } ).Info("Process start tunnel")
        
        c := exec.Command( self.config.mobiledevicePath, "tunnel", "-u", self.uuid, spec )
        
        c.Stdout = os.Stdout
        c.Stderr = os.Stderr
        
        /*err := */c.Run()
        fmt.Printf("mobileDevice tunnel failure\n")
    }()
    
    xctestrunFile := findXctestrun("./bin/wda")
    if xctestrunFile == "" {
        log.Fatal("Could not find WebDriverAgent.xcodeproj or xctestrun of sufficient version")
        return
    }
    o := ProcOptions {
        procName: "wda",
        binary: "xcodebuild",
        startDir: "./bin/wda",
        args: []string{
            "test-without-building",
            "-xctestrun", xctestrunFile,
            "-destination", "id="+self.uuid,
        },
        startFields: log.Fields{
            "testrun": xctestrunFile,
        },
        stdoutHandler: func( line string, plog *log.Entry ) {
            //if debug {
            //    fmt.Printf("[WDA] %s\n", lineStr)
            //}
            if strings.HasPrefix(line, "Test Case '-[UITestingUITests testRunner]' started") {
                plog.Println("[WDA] successfully started")
                self.dev.EventCh <- DevEvent{
                    action: 1,
                }
            }
            if strings.Contains( line, "configuration is unsupported" ) {
                plog.Println( line )
            }
            //fmt.Println( line )
        },
        stderrHandler: func( line string, plog *log.Entry ) {
            if strings.Contains( line, "configuration is unsupported" ) {
                plog.Println( line )
            }
        },
        onStop: func( *Device ) {
            self.dev.EventCh <- DevEvent{
                action: 2,
            }
        },
    }
    
    self.wdaProc = proc_generic( self.devTracker, self.dev, &o )
}

func (self *WDA) stop() {
    if self.wdaProc != nil {
        self.wdaProc.Kill()
        self.wdaProc = nil
    }
}

func findXctestrun(folder string) string {
    iosversion := ""
    
    folder, _ = filepath.EvalSymlinks( folder )
    
    var files []string
    err := filepath.Walk(folder, func( file string, info os.FileInfo, err error ) error {
        if info.IsDir() && folder != file {
            //fmt.Printf("skipping %s\n", file)
            return filepath.SkipDir
        }
        files = append( files, file )
        return nil
    } )
    if err != nil {
        log.Fatal(err)
    }
    
    versionMatch := false
    var findMajor int64 = 0
    var findMinor int64 = 0
    var curMajor int64 = 100
    var curMinor int64 = 100
    if iosversion != "" {
        parts := strings.Split( iosversion, "." )
        findMajor, _ = strconv.ParseInt( parts[0], 10, 64 )
        findMinor, _ = strconv.ParseInt( parts[1], 10, 64 )
        versionMatch = true
    }
    
    xcFile := ""
    for _, file := range files {
        fmt.Printf("Found file %s\n", file )
        if ! strings.HasSuffix(file, ".xctestrun") {
            continue
        }
        
        if ! versionMatch {
            xcFile = file
            break
        }
        
        r := regexp.MustCompile( `iphoneos([0-9]+)\.([0-9]+)` )
        fileParts := r.FindSubmatch( []byte( file ) )
        fileMajor, _ := strconv.ParseInt( string(fileParts[1]), 10, 64 )
        fileMinor, _ := strconv.ParseInt( string(fileParts[2]), 10, 64 )
        
        // Find the smallest file version greater than or equal to the ios version
        // Golang line continuation for long boolean expressions is horrible. :(
        
        // Checked file version smaller than current file version
        // &&
        // Checked file version greater or equal to ios version    
        if ( fileMajor < curMajor  || ( fileMajor == curMajor  && fileMinor <= curMinor  ) ) &&
           ( fileMajor > findMajor || ( fileMajor == findMajor && fileMinor >= findMinor ) ) {
              curMajor = fileMajor
              curMinor = fileMinor
              xcFile = file
        }
    }
    return xcFile
}
