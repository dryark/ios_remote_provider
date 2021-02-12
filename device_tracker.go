package main

import (
    "fmt"
    "strconv"
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
    localPorts []int
    process    map[string] *GenericProc
    lock       *sync.Mutex
    cf         *ControlFloor
    bridge  BridgeRoot
}

func NewDeviceTracker( config *Config ) (*DeviceTracker) {
    self := &DeviceTracker{
        process: make( map[string] *GenericProc ),
        lock: &sync.Mutex{},
        DevMap: make( map [string] *Device ),
        //EventCh: make( chan Event ),
        Config: config,
        localPorts: []int{
            8102, 8103, 8104, 8105, 8106, 8107, 8108, 8109, 8110, 8111, 8112, 8113, 8114, 8115, 8116,
        },
        cf: NewControlFloor( config ),        
    }
    self.bridge = NewIIFBridge(
        func( dev BridgeDev ) ProcTracker { return self.onDeviceConnect1( dev ) },
        func( dev BridgeDev ) { self.onDeviceDisconnect1( dev ) },
        config.iosIfPath,
        self,
    )
    self.cf.DevTracker = self
        
    return self
}

func (self *DeviceTracker) startProc( proc *GenericProc ) {
    self.lock.Lock()
    self.process[ proc.name ] = proc
    self.lock.Unlock()
}

func ( self *DeviceTracker ) stopProc( procName string ) {
    self.lock.Lock()
    delete( self.process, procName )
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

func (self *DeviceTracker) onDeviceConnect1( bdev BridgeDev ) *Device {
    udid := bdev.getUdid()
    fmt.Printf("udid: %s\n", udid)
    //dev := self.DevMap[ udid ]
    
    //devConf := self.Config.devs[ udid ]
    mgInfo := bdev.gestalt( []string{
        "main-screen-width",
        "main-screen-height",
        "main-screen-scale",
    } )
    
    width, _ := strconv.Atoi( mgInfo["main-screen-width"] )
    height, _ := strconv.Atoi( mgInfo["main-screen-height"] )
    scale, _ := strconv.Atoi( mgInfo["main-screen-scale"] )
                    
    self.cf.notifyDeviceExists( udid, width, height, width/scale, height/scale )
    dev := self.onDeviceConnect( udid, bdev )
    self.cf.notifyDeviceInfo( dev )
    dev.startEventLoop()
    dev.openBackupStream()
    dev.startBackupFrameProvider()
    bdev.setProcTracker( self )
    dev.startProcs()
    return dev
}

func (self *DeviceTracker) onDeviceDisconnect1( bdev BridgeDev ) {
    udid := bdev.getUdid()
    dev := self.DevMap[ udid ]
    
    self.onDeviceDisconnect( dev )
    dev.stopEventLoop()
    dev.endProcs()
}

func (self *DeviceTracker) shutdown() {
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

func (self *DeviceTracker) onDeviceConnect( uuid string, bdev BridgeDev ) (*Device){
    dev := self.DevMap[ uuid ]
    if dev != nil {
        dev.connected = true
        return dev
    }
    dev = NewDevice( self.Config, self, uuid, bdev )
    
    devInfo := getAllDeviceInfo( bdev )
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