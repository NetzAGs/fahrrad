// Copyright 2015 Florian Hülsmann <fh@cbix.de>

package main

import (
    "fmt"
    "github.com/mediocregopher/radix.v2/pool"
    //	"golang.org/x/net/icmp"
    "golang.org/x/net/ipv6"
    "net"
    "time"
)

type routerSolicitation struct {
    ip  net.Addr
    mac net.HardwareAddr
}

var (
    pc     *ipv6.PacketConn
    db     *pool.Pool
    // default config, might be overwritten by redis hash key fahrrad/config
    AssignedPrefixLength     uint8          = 64
    OnLinkPrefixLength       uint8          = 48
    DefaultValidLifetime     uint32         = 86400
    DefaultPreferredLifetime uint32         = 14400
    TickerDelay              time.Duration  = 5 * time.Minute
    defaultConfig            map[string]int = map[string]int{
        "AssignedPrefixLength":     int(AssignedPrefixLength),
        "OnLinkPrefixLength":       int(OnLinkPrefixLength),
        "DefaultValidLifetime":     int(DefaultValidLifetime),
        "DefaultPreferredLifetime": int(DefaultPreferredLifetime),
        "TickerDelay":              int(TickerDelay / time.Second),
    }
)

func main() {
    var err error
    // create redis connection pool
    if db, err = pool.New("tcp", "localhost:6379", 10); err != nil {
        panic(err)
    }
    defer db.Empty()
    dbc, err := db.Get()
    if err != nil {
        fmt.Println(err)
    }
    for k, v := range defaultConfig {
        dbc.PipeAppend("HSETNX", "fahrrad/config", k, v)
    }
    for k, _ := range defaultConfig {
        dbc.PipeAppend("HGET", "fahrrad/config", k)
    }
    for _, _ = range defaultConfig {
        dbc.PipeResp()
    }
    var v int
    v, err = dbc.PipeResp().Int()
    if err == nil {
        AssignedPrefixLength = uint8(v)
    }
    v, err = dbc.PipeResp().Int()
    if err == nil {
        OnLinkPrefixLength = uint8(v)
    }
    v, err = dbc.PipeResp().Int()
    if err == nil {
        DefaultValidLifetime = uint32(v)
    }
    v, err = dbc.PipeResp().Int()
    if err == nil {
        DefaultPreferredLifetime = uint32(v)
    }
    v, err = dbc.PipeResp().Int()
    if err == nil {
        TickerDelay = time.Duration(v) * time.Second
    }
    defer db.Put(dbc)

    // open listening connection
    conn, err := net.ListenIP("ip6:ipv6-icmp", &net.IPAddr{net.IPv6unspecified, ""})
    if err != nil {
        panic(err)
    }
    defer conn.Close()
    pc = ipv6.NewPacketConn(conn)
    // RFC4861 requires the hop limit set to 255, but the default value in golang is 64
    pc.SetHopLimit(255)

    // only accept neighbor discovery messages
    filter := new(ipv6.ICMPFilter)
    filter.SetAll(true)
    filter.Accept(ipv6.ICMPTypeRouterSolicitation)
    filter.Accept(ipv6.ICMPTypeRouterAdvertisement)
    filter.Accept(ipv6.ICMPTypeNeighborSolicitation)
    filter.Accept(ipv6.ICMPTypeNeighborAdvertisement)
    if err = pc.SetICMPFilter(filter); err != nil {
        panic(err)
    }

    // read from socket
    buf := make([]byte, 512)
    for {
        n, _, srcAddr, err := pc.ReadFrom(buf)
        if err != nil {
            panic(err)
        }
        go handleND(srcAddr, buf[:n])
    }
}

// method handleND parses arbitrary ICMPv6 messages, currently only router solicitations
func handleND(src net.Addr, body []byte) {
    t := ipv6.ICMPType(body[0])
    fmt.Printf("%v from %v\n", t, src)
    //switch t {
    //case ipv6.ICMPTypeRouterSolicitation:
    // parse ND options
    options, err := parseOptions(body[8:])
    if err != nil {
        fmt.Println(err)
    }
    fmt.Printf("  options: %v\n", options)

    // check if any of the options is a source LLA
    var lla *NDOptionLLA = nil
    for _, o := range options {
        if o == nil {
            continue
        }
        llaopt, ok := (*o).(*NDOptionLLA)
        if !ok {
            continue
        }
        lla = llaopt
        if int(lla.OptionType) != 1 {
            continue
        }
    }
    if lla == nil {
        fmt.Println("no source LLA option given")
        //return
    }
    lladdr := make(net.HardwareAddr, len(lla.Addr))
    copy(lladdr, lla.Addr)
    //default:
    //	return
    //}
}

