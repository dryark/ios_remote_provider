package main

import (
  "os/exec"
  "strings"
  log "github.com/sirupsen/logrus"
  uj "github.com/nanoscopic/ujsonin/mod"
)

func getDeviceName( config *Config, uuid string ) (string) {
    name, _ := exec.Command( config.iosDeployPath, "-i", uuid, "-g", "DeviceName" ).Output()
       
    if name == nil || len(name) == 0 {
        log.WithFields( log.Fields{
            "type": "devinfo_getname_fail",
            "uuid": uuid,
        } ).Error("devinfo getname fail")
        return "unknown"
    }
    nameStr := string( name )
    
    nameStr = nameStr[:len(nameStr)-1]
    return nameStr
}

func getAllDeviceInfo( config *Config, uuid string ) map[string] string {
    info := make( map[string] string )
    
    mainKeys := "DeviceName,EthernetAddress,ModelNumber,HardwareModel,PhoneNumber,ProductType,ProductVersion,UniqueDeviceID,InternationalCircuitCardIdentity,InternationalMobileEquipmentIdentity,InternationalMobileSubscriberIdentity"
    keyArr := strings.Split( mainKeys, "," )
    output, _ := exec.Command( config.iosDeployPath, "-j", "-i", uuid, "-g", mainKeys ).Output()
    root, _ := uj.Parse( output )
    for _, key := range keyArr {
        node := root.Get( key )
        if node != nil {
            info[ key ] = node.String()
        }
    }
    return info
}

func getDeviceInfo( config *Config, uuid string, keyName string ) (string) {
    if( keyName == "" ) {
        keyName = "DeviceName,EthernetAddress,ModelNumber,HardwareModel,PhoneNumber,ProductType,ProductVersion,UniqueDeviceID,InternationalCircuitCardIdentity,InternationalMobileEquipmentIdentity,InternationalMobileSubscriberIdentity"
    } 
    name, _ := exec.Command( config.iosDeployPath, "-i", uuid, "-g", keyName ).Output()
               
    if name == nil || len(name) == 0 {
        log.WithFields( log.Fields{
            "type": "ilib_getinfo_fail",
            "uuid": uuid,
            "key":  keyName,
        } ).Error("ideviceinfo returned nothing")

        return "unknown"
    }
        
    nameStr := string( name )
    nameStr = nameStr[:len(nameStr)-1]
    return nameStr
}

func getFirstDeviceId( config *Config ) ( string ) {
    deviceIds := getDeviceIds( config )
    return deviceIds[0]
}

func getDeviceIds( config *Config ) ( []string ) {
    ids := []string{}
    jsonText, _ := exec.Command( config.iosDeployPath, "-d", "-j", "-t", "1" ).Output()
    root, _ := uj.Parse( []byte( "[" + string(jsonText) + "]" ) )
    
    root.ForEach( func( evNode *uj.JNode ) {
        ev := evNode.Get("Event").String()
        if ev == "DeviceDetected" {
            dev := evNode.Get("Device")
            ids = append( ids, dev.Get("DeviceIdentifier").String() )
        }
    } )
    return ids
}