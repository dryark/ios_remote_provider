package main

import (
    "bufio"
    "crypto/tls"
    "encoding/json"
    "errors"
    "fmt"
    "io/ioutil"
    "net"
    "net/http"
    "net/http/cookiejar"
    "net/url"
    "os"
    "strconv"
    "strings"
    "sync"
    "time"
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

func NewControlFloor( config *Config ) (*ControlFloor, chan bool, chan bool) {
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
    
    stopCf := make( chan bool )
    cfReady := make( chan bool )
    
    go func() {
        exit := false
        delayed := false
        for {
            select {
              case <- stopCf:
                exit = true
                break
              default:
            }
            if exit { break }
            
            success := self.login()
            if success {
                log.WithFields( log.Fields{
                    "type": "cf_login_success",
                } ).Info( "Logged in to control floor" )
                cfReady <- true
            } else {
                fmt.Println("Could not login to control floor")
                fmt.Println("Waiting 10 seconds to retry...")
                time.Sleep( time.Second * 10 )
                fmt.Println("trying again\n")
                delayed = true
                continue
            }
            
            if delayed {
                self.DevTracker.cfReady()
            }
            
            self.openWebsocket()
        }
    }()
    
    return &self, stopCf, cfReady
}

type CFResponse interface {
    asText() (string)
}

type CFR_Pong struct {
    id   int
    text string
}

func (self *CFR_Pong) asText() string {
    return fmt.Sprintf("{id:%d,text:\"%s\"}\n",self.id, self.text)
}

type CFR_Source struct {
    Id     int    `json:"id"`
    Source string `json:"source"`
}

func (self *CFR_Source) asText() string {
    text, _ := json.Marshal( self )
    return string(text)
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
func ( self *ControlFloor ) connectVidChannel( udid string ) *ws.Conn {
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
    
    fmt.Printf("Connected CF imgStream\n")
    
    //dev := self.DevTracker.getDevice( udid )
    
    self.lock.Lock()
    self.vidConns[ udid ] = conn
    self.lock.Unlock()
    
    return conn
    //dev.startStream( conn )
}

// Called from the device object
func ( self *ControlFloor ) destroyVidChannel( udid string ) {
    vidConn := self.vidConns[ udid ]
    
    self.lock.Lock()
    delete( self.vidConns, udid )
    self.lock.Unlock()
    
    vidConn.Close()
}

func ( self *ControlFloor ) openWebsocket() {
    dialer := ws.Dialer{
        Jar: self.cookiejar,
    }
    
    if self.selfSigned {
        log.WithFields( log.Fields{
            "type": "cf_ws_selfsign",
        } ).Warn( "ControlFloor connection is self signed" )
        dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
        //ws.DefaultDialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
    }
    
    log.WithFields( log.Fields{
        "type": "cf_ws_connect",
        "link": ( self.wsBase + "/provider/ws" ),
    } ).Info( "Connecting ControlFloor WebSocket" )
    
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
                //fmt.Printf( "Wrote response back: %s\n", rText )
                if err != nil {
                    fmt.Printf("Error writing to ws\n")
                    break
                }
        }
    } }()
        
    // There is only a single websocket connection between a provider and controlfloor
    // As a result, all messages sent here ought to be small, because if they aren't
    // other messages will be delayed being received and some action started.
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
                        respondChan <- &CFR_Pong{ id: id, text: "done" }
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
                        respondChan <- &CFR_Pong{ id: id, text: "done" }
                    } ()
                } else if mType == "iohid" {
                    udid := root.Get("udid").String()
                    go func() {
                        dev := self.DevTracker.getDevice( udid )
                        if dev != nil {
                            page := root.Get("page").Int()
                            code := root.Get("code").Int()
                            dev.iohid( page, code )
                        }
                    } ()
                } else if mType == "swipe" {
                    udid := root.Get("udid").String()
                    x1 := root.Get("x1").Int()
                    y1 := root.Get("y1").Int()
                    x2 := root.Get("x2").Int()
                    y2 := root.Get("y2").Int()
                    delay := root.Get("delay").Int()
                    go func() {
                        dev := self.DevTracker.getDevice( udid )
                        if dev != nil {
                            dev.swipe( x1, y1, x2, y2, delay )
                        }
                        respondChan <- &CFR_Pong{ id: id, text: "done" }
                    } ()
                } else if mType == "keys" {
                    udid := root.Get("udid").String()
                    keys := root.Get("keys").String()
                    go func() {
                        dev := self.DevTracker.getDevice( udid )
                        if dev != nil {
                            dev.keys( keys )
                        }
                        respondChan <- &CFR_Pong{ id: id, text: "done" }
                    } ()
                } else if mType == "startStream" {
                    udid := root.Get("udid").String()
                    fmt.Printf("Got request to start video stream for %s\n", udid )
                    go func() { self.startVidStream( udid ) }()
                } else if mType == "stopStream" {
                    udid := root.Get("udid").String()
                    go func() { self.stopVidStream( udid ) }()
                } else if mType == "source" {
                    udid := root.Get("udid").String()
                    go func() {
                        dev := self.DevTracker.getDevice( udid )
                        if dev != nil {
                            source := dev.source()
                            respondChan <- &CFR_Source{ Id: id, Source: source } 
                        } else  {
                            respondChan <- &CFR_Pong{ id: id, text: "done" }
                        }
                    } ()
                } else if mType == "shutdown" {
                    do_shutdown( self.config, self.DevTracker )
                }
            }
        }
    }
    
    doneChan <- true
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

func (self *ControlFloor) baseNotify( name string, udid string, variant string, vals url.Values ) {
    ok := self.checkLogin()
    if ok == false {
        panic("Could not login when attempting '" + name + "' notify")
    }
    
    resp, err := self.client.PostForm( self.base + "/provider/device/status/" + variant, vals )
    if err != nil {
        panic( err )
    }
    
    // Ensure the request is closed out
    defer resp.Body.Close()
    ioutil.ReadAll(resp.Body)
    
    if resp.StatusCode != 200 {
        log.WithFields( log.Fields{
            "type": "cf_notify_fail",
            "variant": variant,
            "udid": censorUuid( udid ),
            "values": vals,
            "httpStatus": resp.StatusCode,
        } ).Error( fmt.Sprintf("Failure notifying CF of %s", name) )
    } else {
        log.WithFields( log.Fields{
            "type": "cf_notify",
            "name": name,
            "udid": censorUuid( udid ),
            "values": vals,
        } ).Info( fmt.Sprintf("Notifying CF of %s", name) )
    }
}

func productTypeToCleanName( prodType string ) string {
    if strings.HasPrefix( prodType, "iPhone" ) {
        prodType = prodType[6:]
        typeToName := map[string]string {
            "1,1": "",            "1,2": "3G",         "2,1": "3GS",      "3,1": "4",
            "3,2": "4",           "3,3": "4",          "4,1": "4S",       "4,2": "4S",
            "4,3": "4S",          "5,1": "5",          "5,2": "5",        "5,3": "5C",
            "5,4": "5C",          "6,1": "5S",         "6,2": "5S",       "7,2": "6",
            "7,1": "6 Plus",      "8,1": "6S",         "8,2": "6S Plus",  "8,4": "SE",
            "9,1": "7",           "9,3": "7",          "9,2": "7 Plus",   "9,4": "7 Plus",
            "10,1": "8",          "10,4": "8",         "10,2": "8 Plus",  "10,5": "8 Plus",
            "10,3": "X",          "10,6": "X",         "11,2": "Xs",      "11,4": "Xs Max",
            "11,6": "Xs Max",     "11,8": "Xʀ",        "12,1": "11",      "12,3": "11 Pro",
            "12,5": "11 Pro Max", "12,8": "SE 2",      "13,1": "12 mini", "13,2": "12",
            "13,3": "12 Pro",     "13,4": "12 Pro Max",
        }
        name, exists := typeToName[ prodType ]
        if exists { return "iPhone " + name }
        return prodType
    }
    if strings.HasPrefix( prodType, "iPad" ) {
        prodType = prodType[4:]
        typeToName := map[string]string {
            "1:1": "",              "2:1": "2",             "2:2": "2",             "2:3": "2",
            "2:4": "2",             "3:1": "3",             "3:2": "3",             "3:3": "3",
            "3:4": "4",             "3:5": "4",             "3:6": "4",             "6:11": "5",
            "6:12": "5",            "7:5": "6",             "7:6": "6",             "7:11": "7",
            "7:12": "7",            "11:6": "8",            "11:7": "8",            "4:1": "Air",
            "4:2": "Air",           "4:3": "Air",           "5:3": "Air 2",         "5:4": "Air 2",
            "11:3": "Air 3",        "11:4": "Air 3",        "13:1": "Air 4",        "13:2": "Air 4",
            "2:5": "Mini",          "2:6": "Mini",          "2:7": "Mini",          "4:4": "Mini 2",
            "4:5": "Mini 2",        "4:6": "Mini 2",        "4:7": "Mini 3",        "4:8": "Mini 3",
            "4:9": "Mini 3",        "5:1": "Mini 4",        "5:2": "Mini 4",        "11:1": "Mini 5",
            "11:2": "Mini 5",       "6:3": "Pro 9.7in",     "6:4": "Pro 9.7in",     "7:3": "Pro 10.5in",
            "7:4": "Pro 10.5in",    "8:1": "Pro 11in",      "8:2": "Pro 11in",      "8:3": "Pro 11in",
            "8:4": "Pro 11in",      "8:9": "Pro 11in 2",    "8:10": "Pro 11in 2",   "13:4": "Pro 11in 3",
            "13:5": "Pro 11in 3",   "13:6": "Pro 11in 3",   "13:7": "Pro 11in 3",   "6:7": "Pro 12.9in",
            "6:8": "Pro 12.9in",    "7:1": "Pro 12.9in 2",  "7:2": "Pro 12.9in 2",  "8:5": "Pro 12.9in 3",
            "8:6": "Pro 12.9in 3",  "8:7": "Pro 12.9in 3",  "8:8": "Pro 12.9in 3",  "8:11": "Pro 12.9in 4",
            "8:12": "Pro 12.9in 4", "13:8": "Pro 12.9in 5", "13:9": "Pro 12.9in 5", "13:10": "Pro 12.9in 5",
            "13:11": "Pro 12.9in 5",
        }
        name, exists := typeToName[ prodType ]
        if exists { return "iPhone " + name }
        return prodType
    }
    return prodType
}

func (self *ControlFloor) notifyDeviceInfo( dev *Device, artworkTraits uj.JNode ) {
    info := dev.info
    udid := dev.udid
    str := "{"
    for key, val := range info {
        str = str + fmt.Sprintf("\"%s\":\"%s\",", key, val )
    }
    
    prodDescr := "unknown"
    if artworkTraits != nil {
        prodDescr = artworkTraits.Get("ArtworkDeviceProductDescription").String()
    } else {
        prodDescr = productTypeToCleanName( info[ "ProductType" ] )
    }
    str = str + "\"ArtworkDeviceProductDescription\":\"" + prodDescr + "\"\n"
    str = str + "}"
    
    self.baseNotify("device info", udid, "info", url.Values{
        "udid": {udid},
        "info": {str},
    } )
}

func (self *ControlFloor) notifyDeviceExists( udid string, width int, height int, clickWidth int, clickHeight int ) {
    self.baseNotify("device existence", udid, "exists", url.Values{
        "udid": {udid},
        "width": {strconv.Itoa(width)},
        "height": {strconv.Itoa(height)},
        "clickWidth": {strconv.Itoa(clickWidth)},
        "clickHeight": {strconv.Itoa(clickHeight)},
    } )
}

func (self *ControlFloor) notifyProvisionStopped( udid string ) {
    self.baseNotify("provision stop", udid, "provisionStopped", url.Values{
        "udid": {udid},
    } )
}

/*func (self *ControlFloor) notifyWdaStopped( udid string ) {
    self.baseNotify("WDA stop", udid, "wdaStopped", url.Values{
        "udid": {udid},
    } )
}

func (self *ControlFloor) notifyWdaStarted( udid string ) {
    self.baseNotify("WDA start", udid, "wdaStarted", url.Values{
        "udid": {udid},
    } )
}*/

func (self *ControlFloor) notifyCfaStopped( udid string ) {
    self.baseNotify("CFA stop", udid, "cfaStopped", url.Values{
        "udid": {udid},
    } )
}

func (self *ControlFloor) notifyCfaStarted( udid string ) {
    self.baseNotify("CFA start", udid, "cfaStarted", url.Values{
        "udid": {udid},
    } )
}

func (self *ControlFloor) notifyVideoStopped( udid string ) {
    self.baseNotify("video stop", udid, "videoStopped", url.Values{
        "udid": {udid},
    } )
}

func (self *ControlFloor) notifyVideoStarted( udid string ) {
    self.baseNotify("video start", udid, "videoStarted", url.Values{
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
    
    user := self.config.cfUsername
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
        self.lock.Unlock()
        return false
        //panic( err )
    }
    
    // Ensure the request is closed out
    defer resp.Body.Close()
    ioutil.ReadAll(resp.Body)
    
    success := false
    if resp.StatusCode != 302 {
        success = false
        fmt.Printf("StatusCode from controlfloor login:'%d'\n", resp.StatusCode )
    } else {
        loc, _ := resp.Location()
        
        q := loc.RawQuery
        if q != "fail=1" {
            success = true
        } else {
            fmt.Printf("Location from redirect of controlfloor login:'%s'\n", loc )
        }
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
    resp, err := http.PostForm( protocol + "://" + config.cfHost + "/provider/register",
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
    
    //fmt.Println( string(body) )
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
