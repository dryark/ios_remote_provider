package main

import (
    "sync"
    log "github.com/sirupsen/logrus"
)

type Event struct {
    action     int
    uuid       string    
}

type DeviceTracker struct {
    Config     *Config
    DevMap     map [string] *Device
    EventCh    chan Event
    localPorts []int
    process    map[string] *GenericProc
    lock       *sync.Mutex
    cf         *ControlFloor
}

func NewDeviceTracker( config *Config ) (*DeviceTracker) {
    self := &DeviceTracker{
        process: make( map[string] *GenericProc ),
        lock: &sync.Mutex{},
        DevMap: make( map [string] *Device ),
        EventCh: make( chan Event ),
        Config: config,
        localPorts: []int{
            8102, 8103, 8104, 8105, 8106, 8107, 8108, 8109,
        },
        cf: NewControlFloor( config ),
    }
    self.cf.DevTracker = self
        
    return self
}

func (self *DeviceTracker) procStart( proc *GenericProc ) {
    self.lock.Lock()
    self.process[ proc.name ] = proc
    self.lock.Unlock()
}

func (self *DeviceTracker) getPort() (int) {
    var port int
    self.lock.Lock()
    port, self.localPorts = self.localPorts[0], self.localPorts[1:]
    self.lock.Unlock()
    return port
}

func (self *DeviceTracker) getDevice( udid string ) (*Device) {
    return self.DevMap[ udid ]
}

func (self *DeviceTracker) eventLoop() {
    for {
        select {
        case event := <- self.EventCh:
            udid   := event.uuid
            dev    := self.DevMap[ udid ]
            action := event.action
            
            if action == 0 { // device connect
                devConf := self.Config.devs[ udid ]
                
                self.cf.notifyDeviceExists( udid, devConf.width, devConf.height, devConf.clickWidth, devConf.clickHeight )
                dev = self.onDeviceConnect( udid )
                self.cf.notifyDeviceInfo( dev )
                dev.startEventLoop()
                dev.startProcs()
            } else if action == 1 { // device disconnect
                self.onDeviceDisconnect( dev )
                dev.stopEventLoop()
                dev.endProcs()
            } else if action == 2 { // shutdown
                break
            }
        }
    }
}

func (self *DeviceTracker) shutdown() {
    go func() { self.EventCh <- Event{ action: 2 } }()
    for _,proc := range self.process {
        log.WithFields( log.Fields{
            "type": "shutdown_proc",
            "proc": proc.name,
            "pid":  proc.pid,
        } ).Info("Shutdown proc")
        go func() { proc.Kill() }()
    }
    
    for _,dev := range self.DevMap {
        self.cf.notifyProvisionStopped( dev.uuid )
    }
    
    for _,dev := range self.DevMap {
        log.WithFields( log.Fields{
            "type": "shutdown_device",
            "uuid": censorUuid( dev.uuid ),
        } ).Info("Shutdown device")
        dev.shutdown()
    }
}

func (self *DeviceTracker) onDeviceConnect( uuid string ) (*Device){
    dev := self.DevMap[ uuid ]
    if dev != nil {
        dev.connected = true
        return dev
    }
    dev = NewDevice( self.Config, self, uuid )
    
    devInfo := getAllDeviceInfo( self.Config, uuid )
    log.WithFields( log.Fields{
        "type":       "devInfo",
        "uuid":       censorUuid( uuid ),
        "info": devInfo,
    } ).Info("Device Info")
    
    dev.info = devInfo
    
    self.DevMap[ uuid ] = dev
    return dev
}

func (self *DeviceTracker) onDeviceDisconnect( dev *Device ) {
    dev.connected = false
}