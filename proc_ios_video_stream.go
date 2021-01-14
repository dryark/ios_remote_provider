package main

import (
    "fmt"
    "os"
    "strings"
    "strconv"
    log "github.com/sirupsen/logrus"
)

//func restart_ios_video_stream( dev *RunningDev ) {
//    restart_proc_generic( dev, "ios_video_stream" )
//}

func proc_ios_video_stream( devTracker *DeviceTracker, dev *Device ) {
    udid := dev.uuid
    
    o := ProcOptions{
        procName: "ios_video_stream",
        binary: devTracker.Config.iosVideoStreamPath,
        stderrHandler: func( line string, plog *log.Entry ) {},
        stdoutHandler: func( line string, plog *log.Entry ) {
            if strings.HasPrefix(line,"FIRSTFRAME") {
                line = line[10:]
                firstFrameJSON( devTracker, []byte(line) )
                fmt.Println( line )
            }
        },
    }
    //coordinator := fmt.Sprintf( "127.0.0.1:%d", o.config.Network.Coordinator )
    
    curDir, _ := os.Getwd()
        
    p1str := strconv.Itoa( dev.ivsPort2 )
    p2str := strconv.Itoa( dev.ivsPort3 )
    
    o.args = []string {
        "-stream",
        "-port",        strconv.Itoa( 8000 ),
        "-udid",        udid,
        //"-coordinator", "localhost:8080",
        "-appPort", "8352",
        "-appControlPort", "8351",
        "-localAppPort", p1str,
        "-localAppControlPort", p2str,
        "-mobileDevicePath", ( curDir + "/" + devTracker.Config.mobiledevicePath ),
    }
    
    /*secure := o.config.FrameServer.Secure
    
    if secure {
        cert := o.config.FrameServer.Cert
        key := o.config.FrameServer.Key
        o.args = append( o.args,
            "--secure",
            "--cert", cert,
            "--key",  key,
        )
    }*/
    proc_generic( devTracker, dev, &o )
}