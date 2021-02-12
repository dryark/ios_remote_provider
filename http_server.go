package main

import (
    "bytes"
    "fmt"
    "net/http"
    "strings"
    
    uj "github.com/nanoscopic/ujsonin/mod"
    log "github.com/sirupsen/logrus"
)

func coroHttpServer( devTracker *DeviceTracker ) {
    var listen_addr = fmt.Sprintf( "0.0.0.0:%d", devTracker.Config.httpPort )
    startServer( devTracker, listen_addr )
}

func startServer( devTracker *DeviceTracker, listen_addr string ) {
    log.WithFields( log.Fields{
        "type": "http_start",
    } ).Debug("HTTP server started")

    frameClosure := func( w http.ResponseWriter, r *http.Request ) {
        onFrame( w, r, devTracker )
    }
    
    http.HandleFunc( "/frame", frameClosure )
    
    err := http.ListenAndServe( listen_addr, nil )
    log.WithFields( log.Fields{
        "type": "http_server_fail",
        "error": err,
    } ).Debug("HTTP ListenAndServe Error")
}

func firstFrameJSON( devTracker *DeviceTracker, bytes []byte ) {
    root, _ := uj.Parse( bytes )
    
    msgType := root.Get("type").String()
    
    if msgType == "frame1" {
        width := root.Get("width").Int()
        height := root.Get("height").Int()
        uuid := root.Get("uuid").String()
        devEvent := DevEvent{
            action: 3,
            width: width,
            height: height,
        }
        
        dev := devTracker.DevMap[ uuid ]
        dev.EventCh <- devEvent
    } 
}

func onFrame( w http.ResponseWriter, r *http.Request, devTracker *DeviceTracker ) {
    body := new(bytes.Buffer)
    body.ReadFrom(r.Body)
    bytes := body.Bytes()
    str := string(bytes)
    i := strings.Index( str, "}" )
    fmt.Printf("String to parse:%s\n", str[:i] )
    
    firstFrameJSON( devTracker, bytes )
    
}

func deviceConnect( w http.ResponseWriter, r *http.Request, eventCh chan<- Event ) {
    // signal device loop of device connect
    r.ParseForm()
    uuid := r.Form.Get("uuid")
    fmt.Printf("Device connected: %s\n", uuid )
    eventCh <- Event{
        action: 0,
        uuid: uuid,
    }
}

func deviceDisconnect( w http.ResponseWriter, r *http.Request, eventCh chan<- Event ) {
    // signal device loop of device disconnect
    r.ParseForm()
    uuid := r.Form.Get("uuid")
    fmt.Printf("Device disconnected: %s\n", uuid )
    eventCh <- Event{
        action: 1,
        uuid: uuid,
    }
}
