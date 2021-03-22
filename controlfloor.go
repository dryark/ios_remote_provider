package main

import (
    "bufio"
    "crypto/tls"
    "errors"
    "fmt"
    "io/ioutil"
    "net"
    "net/http"
    "net/http/cookiejar"
    "net/url"
    "os"
    "strconv"
    "sync"
    "reflect"
    log "github.com/sirupsen/logrus"
    uj "github.com/nanoscopic/ujsonin/v2/mod"
    ws "github.com/gorilla/websocket"
)

type ControlFloor struct {
    config     *Config
    ready      bool
    base       string
    wsBase     string
    cookiejar  *cookiejar.Jar
    client     *http.Client
    root       uj.JNode
    pass       string
    lock       *sync.Mutex
    DevTracker *DeviceTracker
    vidConns   map[string] *ws.Conn
    selfSigned bool
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
    
    client := &http.Client{
        Jar: jar,
        CheckRedirect: func(req *http.Request, via []*http.Request) error {
            return http.ErrUseLastResponse
        },
    }
    
    self := ControlFloor{
        config: config,
        ready: false,
        base: "http://" + config.cfHost,
        wsBase: "ws://" + config.cfHost,
        cookiejar: jar,
        client: client,
        pass: pass,
        lock: &sync.Mutex{},
        vidConns: make( map[string] *ws.Conn ),
    }
    if config.https {
        self.base = "https://" + config.cfHost
        self.wsBase = "wss://" + config.cfHost
        if config.selfSigned {
            self.selfSigned = true
            tr := &http.Transport{
                TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
                ForceAttemptHTTP2: false,
            }
            client.Transport = tr
        }
    } else {
        self.base = "http://" + config.cfHost
        self.wsBase = "ws://" + config.cfHost
    }
    
    success := self.login()
    if success {
        fmt.Println("Logged in to control floor")
    } else {
        fmt.Println("Could not login to control floor")
    }
    
    self.openWebsocket()
    
    return &self
}

type CFResponse interface {
    asText() (string)
}

type CFR_Pong struct {
    id int
    text string
}

func (self *CFR_Pong) asText() string {
    return fmt.Sprintf("{id:%d,text:\"%s\"}\n",self.id, self.text)
}

func ( self *ControlFloor ) startVidStream( udid string ) {
    dev := self.DevTracker.getDevice( udid )
    dev.startVidStream()
}

func ( self *ControlFloor ) stopVidStream( udid string ) {
    dev := self.DevTracker.getDevice( udid )
    dev.stopVidStream()
}

// Called from the device object
func ( self *ControlFloor ) startAppStream( udid string ) ( *ws.Conn ) {
    dialer := ws.Dialer{
        Jar: self.cookiejar,
    }
    
    if self.selfSigned {
        fmt.Printf("self signed option\n")
        dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
        //ws.DefaultDialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
    }
    
    fmt.Printf("Connecting to CF imgStream\n")
    conn, _, err := dialer.Dial( self.wsBase + "/provider/imgStream?udid=" + udid, nil )
    if err != nil {
        panic( err )
    }
    
    fmt.Printf("Connected to cf for imgStream\n")
    
    //dev := self.DevTracker.getDevice( udid )
    
    self.lock.Lock()
    self.vidConns[ udid ] = conn
    self.lock.Unlock()
    
    return conn
    //dev.startStream( conn )
}

// Called from the device object
func ( self *ControlFloor ) stopAppStream( udid string ) {
    vidConn := self.vidConns[ udid ]
    vidConn.Close()
    
    self.lock.Lock()
    delete( self.vidConns, udid )
    self.lock.Unlock()
}

func ( self *ControlFloor ) openWebsocket() {
    dialer := ws.Dialer{
        Jar: self.cookiejar,
    }
    
    if self.selfSigned {
        fmt.Printf("self signed option\n")
        dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
        //ws.DefaultDialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
    }
    
    fmt.Printf("link for dialer = %s\n", self.wsBase + "/provider/ws" )
    conn, _, err := dialer.Dial( self.wsBase + "/provider/ws", nil )
    if err != nil {
        panic( err )
    }
    
    respondChan := make( chan CFResponse )
    doneChan := make( chan bool )
    // response channel exists so that multiple threads can queue
    //   responses. WriteMessage is not thread safe
    go func() { for {
        select {
            case <- doneChan:
                break
            case resp := <- respondChan:
                rText := resp.asText()
                err := conn.WriteMessage( ws.TextMessage, []byte(rText) )
                if err != nil {
                    fmt.Printf("Error writing to ws\n")
                    break
                }
        }
    } }()
        
    // There is only a single websocket connection between a provider and controlfloor
    // As a result, all messages sent here ought to be small, because if they aren't
    // other messages will be delayed being received and some action started.
    go func() {
        for {
            t, msg, err := conn.ReadMessage()
            if err != nil {
                fmt.Printf("Error reading from ws\n")
                break
            }
            if t == ws.TextMessage {
                //tMsg := string( msg )
                b1 := []byte{ msg[0] }
                if string(b1) == "{" {
                    root, _ := uj.Parse( msg )
                    id := root.Get("id").Int()
                    mType := root.Get("type").String()
                    if mType == "ping" {
                        respondChan <- &CFR_Pong{ id: id, text: "pong" }
                    } else if mType == "click" {
                        udid := root.Get("udid").String()
                        x := root.Get("x").Int()
                        y := root.Get("y").Int()
                        go func() {
                            dev := self.DevTracker.getDevice( udid )
                            if dev != nil {
                                dev.clickAt( x, y )
                            }
                        } ()
                    } else if mType == "hardPress" {
                        udid := root.Get("udid").String()
                        x := root.Get("x").Int()
                        y := root.Get("y").Int()
                        go func() {
                            dev := self.DevTracker.getDevice( udid )
                            if dev != nil {
                                dev.hardPress( x, y )
                            }
                        } ()
                    } else if mType == "longPress" {
                        udid := root.Get("udid").String()
                        x := root.Get("x").Int()
                        y := root.Get("y").Int()
                        go func() {
                            dev := self.DevTracker.getDevice( udid )
                            if dev != nil {
                                dev.longPress( x, y )
                            }
                        } ()
                    } else if mType == "home" {
                        udid := root.Get("udid").String()
                        go func() {
                            dev := self.DevTracker.getDevice( udid )
                            if dev != nil {
                                dev.home()
                            }
                        } ()
                    } else if mType == "swipe" {
                        udid := root.Get("udid").String()
                        x1 := root.Get("x1").Int()
                        y1 := root.Get("y1").Int()
                        x2 := root.Get("x2").Int()
                        y2 := root.Get("y2").Int()
                        go func() {
                            dev := self.DevTracker.getDevice( udid )
                            if dev != nil {
                                dev.swipe( x1, y1, x2, y2 )
                            }
                        } ()
                    } else if mType == "keys" {
                        udid := root.Get("udid").String()
                        keys := root.Get("keys").String()
                        go func() {
                            dev := self.DevTracker.getDevice( udid )
                            if dev != nil {
                                dev.keys( keys )
                            }
                        } ()
                    } else if mType == "startStream" {
                        udid := root.Get("udid").String()
                        fmt.Printf("Got request to start video stream for %s\n", udid )
                        go func() { self.startVidStream( udid ) }()
                    } else if mType == "stopStream" {
                        udid := root.Get("udid").String()
                        go func() { self.stopVidStream( udid ) }()
                    }
                }
            }
        }
        doneChan <- true
    }()    
}

func loadCFConfig( configPath string ) (uj.JNode) {
    fh, serr := os.Stat( configPath )
    if serr != nil {
        log.WithFields( log.Fields{
            "type":        "err_read_config",
            "error":       serr,
            "config_path": configPath,
        } ).Fatal(
          "Could not read ControlFloor auth token. Have you run `./main register`?",
        )
    }
    configFile := configPath
    switch mode := fh.Mode(); {
        case mode.IsDir(): configFile = fmt.Sprintf("%s/config.json", configPath)
    }
    content, err := ioutil.ReadFile( configFile )
    if err != nil { log.Fatal( err ) }
	
    root, _, perr := uj.ParseFull( content )
    if perr != nil {
        log.WithFields( log.Fields{
            "error": perr,
        } ).Fatal(
            "ControlFloor auth token is invalid. Rerun `./main register`",
        )
    }
    
    return root
}

func writeCFConfig( configPath string, pass string ) {
    bytes := []byte(fmt.Sprintf("{pass:\"%s\"}\n",pass))
    err := ioutil.WriteFile( configPath, bytes, 0644)
    if err != nil {
        panic( err )
    }
}

func (self *ControlFloor) baseNotify( name string, udid string, vals url.Values ) {
    ok := self.checkLogin()
    if ok == false {
        panic("Could not login when attempting '" + name + "' notify")
    }
    
    resp, err := self.client.PostForm( self.base + "/provider/devStatus", vals )
    if err != nil {
        panic( err )
    }
    
    if resp.StatusCode != 200 {
        fmt.Printf("Got status %d from '%s' notify\n", resp.StatusCode, name )
    } else {
        fmt.Printf("Notified control floor of '%s'; uuid=%s\n", name, censorUuid( udid ) )
    }
}

func (self *ControlFloor) notifyDeviceInfo( dev *Device ) {
    info := dev.info
    udid := dev.udid
    str := "{"
    for key, val := range info {
        str = str + fmt.Sprintf("\"%s\":\"%s\",", key, val )
    }
    str = str + "}"
    
    self.baseNotify("device info", udid, url.Values{
        "status": {"info"},
        "udid": {udid},
        "info": {str},
    } )
}

func (self *ControlFloor) notifyDeviceExists( udid string, width int, height int, clickWidth int, clickHeight int ) {
    self.baseNotify("device existence", udid, url.Values{
        "status": {"exists"},
        "udid": {udid},
        "width": {strconv.Itoa(width)},
        "height": {strconv.Itoa(height)},
        "clickWidth": {strconv.Itoa(clickWidth)},
        "clickHeight": {strconv.Itoa(clickHeight)},
    } )
}

func (self *ControlFloor) notifyProvisionStopped( udid string ) {
    self.baseNotify("provision stop", udid, url.Values{
        "status": {"provisionStopped"},
        "udid": {udid},
    } )
}

func (self *ControlFloor) notifyWdaStopped( udid string ) {
    self.baseNotify("wda stop", udid, url.Values{
        "status": {"wdaStopped"},
        "udid": {udid},
    } )
}

func (self *ControlFloor) notifyWdaStarted( udid string ) {
    self.baseNotify("wda start", udid, url.Values{
        "status": {"wdaStarted"},
        "udid": {udid},
    } )
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
    pass := self.pass
    
    resp, err := self.client.PostForm( self.base + "/provider/login",
        url.Values{
            "user": {user},
            "pass": {pass},
        },
    )
    if err != nil {
        var urlError *url.Error
        if errors.As( err, &urlError ) {
          var netOpError *net.OpError 
          if errors.As( urlError, &netOpError ) {
            rootErr := netOpError.Err
            if( rootErr.Error() == "connect: connection refused" ) {
              fmt.Printf("Could not connect to ControlFarm; is it running?\n")
            } else {
              fmt.Printf("Err type:%s - %s\n", reflect.TypeOf(err), err )
              fmt.Printf("urlError type:%s - %s\n", reflect.TypeOf(urlError), urlError );
              fmt.Printf("netOpError type:%s - %s\n", reflect.TypeOf(netOpError), netOpError )
            }
          } else {
            fmt.Printf("Err type:%s - %s\n", reflect.TypeOf(err), err )
            fmt.Printf("urlError type:%s - %s\n", reflect.TypeOf(urlError), urlError );
          }
        } else {
          fmt.Printf("Err type:%s - %s\n", reflect.TypeOf(err), err )
        }
        panic( err )
    }
    
    success := false
    if resp.StatusCode != 302 {
        success = false
        fmt.Printf("StatusCode from controlfloor login:'%d'\n", resp.StatusCode )
    } else {
        loc, _ := resp.Location()
        fmt.Printf("Location from redirect of controlfloor login:'%s'\n", loc )
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
    protocol := "http"
    if config.https {
        protocol = "https"
    }
    resp, err := http.PostForm( protocol + "://" + config.cfHost + "/register",
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
    
    fmt.Println( string(body) )
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
    
    writeCFConfig( "cf.json", pass )
            
    return pass
}