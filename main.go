package main

import (
    //"bufio"
    "flag"
    //"fmt"
    //"io/ioutil"
    "os"
    "os/signal"
    "syscall"
    "sync"
    //"net/http"
    //"net/url"
    log "github.com/sirupsen/logrus"
    //uj "github.com/nanoscopic/ujsonin/mod"
)

type Device struct {
    uuid        string
    name        string
    lock        *sync.Mutex
    wdaPort     int
    ivsPort1    int
    ivsPort2    int
    ivsPort3    int
    iosVersion  string
    productType string
    productNum  string
    vidWidth    int
    vidHeight   int
    process     map[string] *GenericProc
    owner       string
    connected   bool
    EventCh     chan DevEvent
    wda         *WDA
    devTracker  *DeviceTracker
    config      *Config
    cf          *ControlFloor
    info        map[string] string
}

type DevEvent struct {
    action int
    width      int
    height     int
}

type Event struct {
    action     int
    uuid       string    
}

func NewDevice( config *Config, devTracker *DeviceTracker, uuid string ) (*Device) {
    dev := Device{
        devTracker: devTracker,
        wdaPort:    devTracker.getPort(),
        ivsPort1:   devTracker.getPort(),
        ivsPort2:   devTracker.getPort(),
        ivsPort3:   devTracker.getPort(),
        config:     config,
        uuid:       uuid,
        lock:       &sync.Mutex{},
        process:    make( map[string] *GenericProc ),
        cf:         devTracker.cf,
        EventCh:    make( chan DevEvent ),
    }
    return &dev
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
    self := DeviceTracker{
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
        
    return &self
}

func main() {
    var debug      = flag.Bool(   "debug" , false        , "Use debug log level" )
    var warn       = flag.Bool(   "warn"  , false        , "Use warn log level" )
    var configPath = flag.String( "config", "config.json", "Config file path" )
    var register   = flag.Bool(   "register", false      , "Register against control floor" )
    flag.Parse()
    
    setupLog( *debug, *warn )
    
    config := NewConfig( *configPath )
    
    if *register {
        doregister( config )
        return
    }
        
    //useAllDevs := false
    //devs := loadDevsFromConfig( config )
    //if devs == nil { useAllDevs = true }
    
    devTracker := NewDeviceTracker( config )
    coro_sigterm( config, devTracker )
    
    coroHttpServer( devTracker )
    
    proc_device_trigger( devTracker )
    
    devTracker.eventLoop()
}

func setupLog( debug bool, warn bool ) {
    //log.SetFormatter(&log.JSONFormatter{})
    log.SetOutput(os.Stdout)
    if debug {
        log.SetLevel( log.DebugLevel )
    } else if warn {
        log.SetLevel( log.WarnLevel )
    } else {
        log.SetLevel( log.InfoLevel )
    }
}

func censorUuid( uuid string ) (string) {
    return "***" + uuid[len(uuid)-4:]
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

func (self *DeviceTracker) eventLoop() {
    for {
        select {
        case event := <- self.EventCh:
            uuid   := event.uuid
            dev    := self.DevMap[ uuid ]
            action := event.action
            
            if action == 0 { // device connect
                self.cf.notifyDeviceExists( uuid )
                dev = self.onDeviceConnect( uuid )
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

func (self *Device) shutdown() {
    go func() { self.EventCh <- DevEvent{ action: 0 } }()
    for _,proc := range self.process {
        log.WithFields( log.Fields{
            "type": "shutdown_dev_proc",
            "uuid": censorUuid( self.uuid ),
            "proc": proc.name,
            "pid":  proc.pid,
        } ).Info("Shutdown proc")
        go func() { proc.Kill() }()
    }
}

func (self *Device) startEventLoop() {
    go func() {
        DEVEVENTLOOP:
        for {
            select {
            case event := <- self.EventCh:
                action := event.action
                if action == 0 { // stop event loop
                    break DEVEVENTLOOP
                } else if action == 1 { // WDA started
                    self.cf.notifyWdaStarted( self.uuid )
                    // start video streaming
                } else if action == 2 { // WDA stopped
                    
                } else if action == 3 { // first video frame
                    self.onFirstFrame( &event )
                }
            }
        }
    }()
}

func (self *Device) stopEventLoop() {
    self.EventCh <- DevEvent{
        action: 0,
    }
}

func (self *Device) startProcs() {
    // start wda
    self.wda = NewWDA( self.config, self.devTracker, self, self.wdaPort )
    proc_ios_video_stream( self.devTracker, self )
    // start frame queue
}

func (self *Device) endProcs() {
}

func (self *Device) onFirstFrame( event *DevEvent ) {
    self.vidWidth = event.width
    self.vidWidth = event.height
    log.WithFields( log.Fields{
        "type":       "first_frame",
        "proc":       "ios_video_stream",
        "width":      self.vidWidth,
        "height":     self.vidWidth,
        "uuid":       censorUuid( self.uuid ),
    } ).Info("Video - first frame")
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

func loadDevsFromConfig() (*string) {
    return nil
    /*devs.ForEach( func( conf *uj.JNode ) {
        oneid := conf.Get("udid").String()
        if oneid == udid {
            dev.Width = conf.Get("width").Int()
            dev.Height = conf.Get("height").Int()
        }
    } )*/
}

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
            "state": "done",
        } ).Info("Shutdown finished")

        os.Exit(0)
    }()
}