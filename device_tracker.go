package main

import (
    "fmt"
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
    cfStop     chan bool
    bridge     BridgeRoot
    pendingDevs [] BridgeDev
}

func NewDeviceTracker( config *Config, detect bool ) (*DeviceTracker) {
    var cf *ControlFloor
    var cfStop chan bool
    if detect {
      cf, cfStop = NewControlFloor( config )
    }
    self := &DeviceTracker{
        process: make( map[string] *GenericProc ),
        lock: &sync.Mutex{},
        DevMap: make( map [string] *Device ),
        //EventCh: make( chan Event ),
        Config: config,
        localPorts: []int{
            8102, 8103, 8104, 8105, 8106, 8107, 8108, 8109, 8110, 8111, 8112, 8113, 8114, 8115, 8116,
        },
        cf: cf,
        cfStop: cfStop,
    }
    self.bridge = NewIIFBridge(
        config,
        func( dev BridgeDev ) ProcTracker { return self.onDeviceConnect1( dev ) },
        func( dev BridgeDev ) { self.onDeviceDisconnect1( dev ) },
        config.iosIfPath,
        self,
        detect,
    )
    if detect {
        cf.DevTracker = self
    }
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

func (self *DeviceTracker) cfReady() {
    fmt.Println("Starting delayed devices:")
    for _, bdev := range self.pendingDevs {
        fmt.Printf("Delayed device - udid: %s\n", bdev.getUdid() )
        self.onDeviceConnect1( bdev )
    }
    self.pendingDevs = []BridgeDev{}
}

func (self *DeviceTracker) onDeviceConnect1( bdev BridgeDev ) *Device {
    udid := bdev.getUdid()
    
    if !self.cf.ready {
        self.pendingDevs = append( self.pendingDevs, bdev )
        fmt.Printf("Device attached, but ControlFloor not ready.\n  udid=%s\n", udid )
        return nil
    }
    
    fmt.Printf("udid: %s\n", udid)
    //dev := self.DevMap[ udid ]
    
    _, devConfOk := self.Config.devs[udid]
        
    mgInfo := bdev.gestaltnode( []string{
        "AvailableDisplayZoomSizes",
        "main-screen-width",
        "main-screen-height",
        "ArtworkTraits",
    } )
    width := mgInfo["main-screen-width"].Int()
    height := mgInfo["main-screen-height"].Int()
    
    var clickWidth int
    var clickHeight int
    
    var devConf *CDevice
    if devConfOk {
      devConfOb := self.Config.devs[udid]
      devConf = &devConfOb
    }
    if devConfOk && devConf.uiWidth != 0 {
        devConf := self.Config.devs[ udid ]
        clickWidth = devConf.uiWidth
        clickHeight = devConf.uiHeight
    } else {
        sizeArr := mgInfo["AvailableDisplayZoomSizes"].Get("default") // zoomed also available
        clickWidth = sizeArr.GetAt(1).Int()
        clickHeight = sizeArr.GetAt(3).Int()
    }
        
    self.cf.notifyDeviceExists( udid, width, height, clickWidth, clickHeight )
    dev := self.onDeviceConnect( udid, bdev )
    self.cf.notifyDeviceInfo( dev, mgInfo["ArtworkTraits"] )
    bdev.setProcTracker( self )
    dev.startup()
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
        self.cf.notifyProvisionStopped( dev.udid )
    }
    
    for _,dev := range self.DevMap {
        log.WithFields( log.Fields{
            "type": "shutdown_device",
            "uuid": censorUuid( dev.udid ),
        } ).Info("Shutdown device")
        dev.shutdown()
    }
    
    go func() { self.cfStop <- true }()
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