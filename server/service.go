package server

import (
	"HomeRover/consts"
	"HomeRover/models/client"
	"HomeRover/models/config"
	"HomeRover/models/data"
	"HomeRover/models/mode"
	"HomeRover/models/server"
	"fmt"
	mapset "github.com/deckarep/golang-set"
	"net"
	"sync"
)

type Service struct {
	conf 				*config.ServerConfig
	confMu				sync.RWMutex

	Groups				map[uint16]*server.Group
	TransMu				sync.RWMutex
	clientMu			sync.RWMutex

	serviceAddr			*net.UDPAddr
	forwardAddr			*net.UDPAddr

	serviceConn 		*net.UDPConn
	forwardConn			*net.UDPConn
}

func (s *Service) init() error {
	groupSet := mapset.NewSet()
	groupSet.Add(uint16(1))
	groupSet.Add(uint16(2))
	s.Groups[0] = &server.Group{
		Id: 0,
		Rover: client.Client{
			Info: client.Info{Id: 1},
		},
		Controller: client.Client{
			Info: client.Info{Id: 2},
		},
	}

	var err error
	s.confMu.RLock()
	s.serviceAddr, err = net.ResolveUDPAddr("udp", fmt.Sprintf("0.0.0.0:%s", s.conf.ServicePort))
	if err != nil {
		return err
	}
	s.serviceConn, err = net.ListenUDP("udp", s.serviceAddr)
	if err != nil {
		return err
	}
	s.forwardAddr, err = net.ResolveUDPAddr("udp", fmt.Sprintf("0.0.0.0:%s", s.conf.ForwardPort))
	if err != nil {
		return err
	}
	s.confMu.RUnlock()

	return nil
}

func (s *Service)listenClients()  {
	recvBytes := make([]byte, s.conf.PackageLen)
	recvData := data.Data{}
	var (
		err        error
		addr       *net.UDPAddr
		clientInfo client.Info
	)

	for {
		_, addr, err = s.serviceConn.ReadFromUDP(recvBytes)
		if err != nil {
			fmt.Println(err)
		}
		err = recvData.FromBytes(recvBytes)
		if err != nil {
			fmt.Println(err)
		}

		if recvData.Type == consts.Service {
			fmt.Println("heartbeat received")
			err = clientInfo.FromBytes(recvData.Payload)
			if err != nil {
				fmt.Println(err)
			}

			if recvData.Channel == consts.Controller {
				// get the dest client from s.Groups
				s.clientMu.Lock()
				s.Groups[clientInfo.GroupId].Controller.Info = clientInfo
				s.Groups[clientInfo.GroupId].Controller.State = consts.Online
				s.clientMu.Unlock()

				s.TransMu.Lock()
				s.Groups[clientInfo.GroupId].Trans = &clientInfo.Trans
				s.TransMu.Unlock()

				// send rover addr back
				recvData.Payload, err = makeRespClientBytes(
					&s.Groups[clientInfo.GroupId].Rover,
					s.Groups[clientInfo.GroupId].Trans,
					s.forwardAddr,
				)
				if err != nil {
					fmt.Println(err)
				}
			} else if recvData.Channel == consts.Service {
				// get the dest client from s.Groups
				s.clientMu.Lock()
				s.Groups[clientInfo.GroupId].Rover.Info = clientInfo
				s.Groups[clientInfo.GroupId].Rover.State = consts.Online
				s.clientMu.Unlock()

				// send controller addr back
				recvData.Payload, err = makeRespClientBytes(
					&s.Groups[clientInfo.GroupId].Controller,
					s.Groups[clientInfo.GroupId].Trans,
					s.forwardAddr,
				)
				if err != nil {
					fmt.Println(err)
				}
			}

			recvData.Type = consts.Server
			recvData.Channel = consts.Service
			_, err = s.serviceConn.WriteToUDP(recvData.ToBytes(), addr)
			if err != nil {
				fmt.Println(err)
			}
		}
	}
}

func (s *Service) forward()  {
	recvBytes := make([]byte, s.conf.PackageLen)
	var (
		err			error
		addr		*net.UDPAddr
		recvData	data.Data
		recvEntity	data.EntityData
	)

	for {
		_, _, err = s.serviceConn.ReadFromUDP(recvBytes)
		if err != nil {
			fmt.Println(err)
		}

		err = recvData.FromBytes(recvBytes)
		if err != nil {
			fmt.Println(err)
		}

		err = recvEntity.FromBytes(recvData.Payload)
		if err != nil {
			fmt.Println(err)
		}

		switch recvData.Channel {
		case consts.Cmd:
			if recvData.Type == consts.Controller {
				addr = s.Groups[recvEntity.GroupId].Rover.Info.CmdAddr
			} else {
				addr = s.Groups[recvEntity.GroupId].Controller.Info.CmdAddr
			}
		case consts.Video:
			if recvData.Type == consts.Controller {
				addr = s.Groups[recvEntity.GroupId].Rover.Info.VideoAddr
			} else {
				addr = s.Groups[recvEntity.GroupId].Controller.Info.VideoAddr
			}
		case consts.Audio:
			if recvData.Type == consts.Controller {
				addr = s.Groups[recvEntity.GroupId].Rover.Info.AudioAddr
			} else {
				addr = s.Groups[recvEntity.GroupId].Controller.Info.AudioAddr
			}
		}

		_, err = s.forwardConn.WriteToUDP(recvBytes, addr)
		if err != nil {
			fmt.Println(err)
		}
	}
}

func (s *Service) Run() {
	err := s.init()
	if err != nil {
		fmt.Println(err)
	}
	
	go s.listenClients()
}

func makeRespClientBytes(c *client.Client, transRule *mode.Trans, forwardAddr *net.UDPAddr) ([]byte, error) {
	respClient := client.Client{
		State: c.State,
		Info:  client.Info{},
	}

	if transRule.Cmd {
		// if cmd channel is HoldPunching mode
		respClient.Info.CmdAddr = c.Info.CmdAddr
	} else {
		// cmd channel is forwarding mode
		respClient.Info.CmdAddr = forwardAddr
	}

	if transRule.Video {
		respClient.Info.VideoAddr = c.Info.VideoAddr
	} else {
		respClient.Info.VideoAddr = forwardAddr
	}

	if transRule.Audio {
		respClient.Info.AudioAddr = c.Info.AudioAddr
	} else {
		respClient.Info.AudioAddr = forwardAddr
	}

	respBytes, err := respClient.ToBytes()
	if err != nil {
		return nil, err
	}

	return respBytes, nil
}