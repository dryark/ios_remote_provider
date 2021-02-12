package main

import (
  "strings"
  //log "github.com/sirupsen/logrus"
  //uj "github.com/nanoscopic/ujsonin/mod"
)

func getDeviceName( bridge BridgeDev ) (string) {
    info := bridge.info( []string{ "DeviceName" } )
    return info["DeviceName"]
}

func getAllDeviceInfo( bridge BridgeDev ) map[string] string {
    mainKeys := "DeviceName,EthernetAddress,ModelNumber,HardwareModel,PhoneNumber,ProductType,ProductVersion,UniqueDeviceID,InternationalCircuitCardIdentity,InternationalMobileEquipmentIdentity,InternationalMobileSubscriberIdentity"
    keyArr := strings.Split( mainKeys, "," )
    return bridge.info( keyArr )
}

func getDeviceInfo( bridge BridgeDev, keyName string ) map[string] string {
    if( keyName == "" ) {
        keyName = "DeviceName,EthernetAddress,ModelNumber,HardwareModel,PhoneNumber,ProductType,ProductVersion,UniqueDeviceID,InternationalCircuitCardIdentity,InternationalMobileEquipmentIdentity,InternationalMobileSubscriberIdentity"
    }
    keyArr := strings.Split( keyName, "," )
    return bridge.info( keyArr )
}

func getFirstDeviceId( root BridgeRoot ) ( string ) {
    return getDeviceIds( root )[0]
}

func getDeviceIds( root BridgeRoot ) ( []string ) {
    devs := root.list()
    ids := []string{}
    for _,dev := range devs {
        ids = append( ids, dev.udid )
    }
    return ids
}