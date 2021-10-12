package main

import (
    "fmt"
    log "github.com/sirupsen/logrus"
)

type WDA struct {
    udid          string
    devTracker    *DeviceTracker
    dev           *Device
    config        *Config
    base          string
    startChan     chan int
    wdaPort       int
}

func NewWDA( config *Config, devTracker *DeviceTracker, dev *Device ) (*WDA) {
    self := NewWDANoStart( config, devTracker, dev )
    
    //if config.wdaMethod != "manual" {
        self.start( nil )
    //} else {
    //    self.forward( func( x int, stopChan chan bool ) {
    //    } )
    //}
    return self
}

func NewWDANoStart( config *Config, devTracker *DeviceTracker, dev *Device ) (*WDA) {
    self := WDA{
        udid:          dev.udid,
        wdaPort:       dev.wdaPort,
        devTracker:    devTracker,
        dev:           dev,
        config:        config,
        base:          fmt.Sprintf("http://127.0.0.1:%d",dev.wdaPort),
    }
    
    return &self
}

func (self *WDA) forward( onready func( int, chan bool ) ) {
    pairs := []TunPair{
        TunPair{ from: self.wdaPort, to: 8100 },
    }
    
    stopChan := make( chan bool )
    self.dev.bridge.tunnel( pairs, func() {
        if onready != nil {
            onready( 0, stopChan )
        }
    } )
}

func (self *WDA) start( started func( int, chan bool ) ) {
    self.forward( func( x int, stopChan chan bool ) {
        self.dev.bridge.wda(
            func() { // onStart
                log.WithFields( log.Fields{
                    "type": "wda_start",
                    "udid":  censorUuid(self.udid),
                    "port": self.wdaPort,
                } ).Info("[WDA] successfully started")
                
                //if self.startChan != nil {
                //    self.startChan <- 0
                //}
                
                self.dev.EventCh <- DevEvent{ action: DEV_WDA_START }
            },
            func(interface{}) { // onStop
                self.dev.EventCh <- DevEvent{ action: DEV_WDA_STOP }
            },
        )
    } )
}