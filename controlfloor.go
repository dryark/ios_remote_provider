package main

import (
    "bufio"
    "fmt"
    "io/ioutil"
    "net/http"
    "net/http/cookiejar"
    "net/url"
    "os"
    "sync"
    log "github.com/sirupsen/logrus"
    uj "github.com/nanoscopic/ujsonin/mod"
)

type ControlFloor struct {
    config    *Config
    ready     bool
    base      string
    cookiejar *cookiejar.Jar
    client    *http.Client
    root      *uj.JNode
    pass      string
    lock      *sync.Mutex
}

func NewControlFloor( config *Config ) (*ControlFloor) {
    jar, err := cookiejar.New(&cookiejar.Options{})
    if err != nil {
        panic( err )
    }
    
    root := loadCFConfig( "cf.json" )
    passNode := root.Get("pass")
    if passNode == nil {
    }
    
    pass := passNode.String()
    
    client := &http.Client{ Jar: jar }
    
    self := ControlFloor{
        config: config,
        ready: false,
        base: "http://" + config.cfHost,
        cookiejar: jar,
        client: client,
        pass: pass,
        lock: &sync.Mutex{},
    }
    
    success := self.login()
    if success {
        fmt.Println("Logged in to control floor")
    } else {
        fmt.Println("Could not login to control floor")
    }
    
    return &self
}

func loadCFConfig( configPath string ) (*uj.JNode) {
    fh, serr := os.Stat( configPath )
    if serr != nil {
        log.WithFields( log.Fields{
            "type":        "err_read_config",
            "error":       serr,
            "config_path": configPath,
        } ).Fatal("Could not read specified config path")
    }
    configFile := configPath
    switch mode := fh.Mode(); {
        case mode.IsDir(): configFile = fmt.Sprintf("%s/config.json", configPath)
    }
    content, err := ioutil.ReadFile( configFile )
	if err != nil { log.Fatal( err ) }
	
    root, _ := uj.Parse( content )
    
    return root
}

func (self *ControlFloor) notifyDeviceInfo( dev *Device ) {
    ok := self.checkLogin()
    if ok == false {
        panic("Could not login when attempting to notify of device info")
    }
    
    info := dev.info
    uuid := dev.uuid
    str := "{"
    for key, val := range info {
        str = str + fmt.Sprintf("\"%s\":\"%s\",", key, val )
    }
    str = str + "}"
    
    _, err := self.client.PostForm( self.base + "/provider/devStatus",
        url.Values{
            "status": {"info"},
            "uuid": {uuid},
            "info": {str},
        },
    )
    if err != nil {
        panic( err )
    }
}

func (self *ControlFloor) notifyDeviceExists( uuid string ) {
    ok := self.checkLogin()
    if ok == false {
        panic("Could not login when attempting to notify of device existence")
    }
    
    resp, err := self.client.PostForm( self.base + "/provider/devStatus",
        url.Values{
            "status": {"exists"},
            "uuid": {uuid},
        },
    )
    if err != nil {
        panic( err )
    }
    
    if resp.StatusCode != 200 {
        fmt.Printf("Got status %d from device existence notify\n", resp.StatusCode )
    } else {
        fmt.Printf("Notified control floor of device existence; uuid=%s\n", censorUuid( uuid ) )
    }
}

func (self *ControlFloor) notifyProvisionStopped( uuid string ) {
    ok := self.checkLogin()
    if ok == false {
        panic("Could not login when attempting to notify of device existence")
    }
    
    resp, err := self.client.PostForm( self.base + "/provider/devStatus",
        url.Values{
            "status": {"provisionStopped"},
            "uuid": {uuid},
        },
    )
    if err != nil {
        panic( err )
    }
    
    if resp.StatusCode != 200 {
        fmt.Printf("Got status %d from device existence notify\n", resp.StatusCode )
    } else {
        fmt.Printf("Notified control floor of device existence; uuid=%s\n", censorUuid( uuid ) )
    }
}

func (self *ControlFloor) notifyWdaStarted( uuid string ) {
    ok := self.checkLogin()
    if ok == false {
        panic("Could not login when attempting to notify of device existence")
    }
    
    resp, err := self.client.PostForm( self.base + "/provider/devStatus",
        url.Values{
            "status": {"wdaStarted"},
            "uuid": {uuid},
        },
    )
    if err != nil {
        panic( err )
    }
    
    if resp.StatusCode != 200 {
        fmt.Printf("Got status %d from device existence notify\n", resp.StatusCode )
    } else {
        fmt.Printf("Notified control floor of device existence; uuid=%s\n", censorUuid( uuid ) )
    }
}

func (self *ControlFloor) checkLogin() (bool) {
    self.lock.Lock()
    ready := self.ready
    self.lock.Unlock()
    if ready { return true }
    return self.login()
}

func (self *ControlFloor) login() (bool) {
    self.lock.Lock()
    
    user := "first"
    pass := "e8dfb2789298d64e607fe997470bb88d"
    
    resp, err := self.client.PostForm( self.base + "/provider/login",
        url.Values{
            "user": {user},
            "pass": {pass},
        },
    )
    if err != nil {
        panic( err )
    }
    
    success := false
    if resp.StatusCode != 302 {
        success = true
    } else {
        loc, _ := resp.Location()
        q := loc.RawQuery
        if q != "fail=1" { success = true }
    }
    
    if !success {
        self.ready = false
        self.lock.Unlock()
        return false
    }
    self.ready = true
    self.lock.Unlock()
    return true
}

func doregister( config *Config ) (string) {
    // query cli for registration password
    reader := bufio.NewReader( os.Stdin )
    fmt.Print("Enter registration password:")
    regPass, _ := reader.ReadString('\n')
    if regPass == "\n" {
        regPass = "doreg"
        fmt.Printf("Using default registration password of %s\n", regPass)
    }
    
    username := config.cfUsername
    // send registration to control floor with id and public key
    resp, err := http.PostForm( "http://" + config.cfHost + "/register",
        url.Values{
            "regPass": {regPass},
            "username": {username},
        },
    )
    if err != nil {
        panic( err )
    }
    if resp.Body == nil {
        panic("registration respond body is empty")
    }
    defer resp.Body.Close()
    
    body, readErr := ioutil.ReadAll( resp.Body )
    if readErr != nil {
        panic( readErr )
    }
    
    root, _ := uj.Parse( body )
    
    sNode := root.Get("Success")
    if sNode == nil {
        panic( "No Success node in registration result" )
    }
    success := sNode.Bool()
    if !success {
        panic("Registration failed")
    }
    
    existed := root.Get("Existed").Bool()
    pass := root.Get("Password").String()
    fmt.Printf("Registered and got password %s\n", pass)
    if existed {
        fmt.Printf("User %s existed so password was renewed\n", username )
    }
            
    return pass
}