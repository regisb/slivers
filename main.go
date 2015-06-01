package main

import (
	"bytes"
	"crypto/sha1"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/jackpal/bencode-go"
)

func main() {
	flag.Parse()
	if len(flag.Args()) == 0 {
		flag.Usage()
		os.Exit(1)
	}
	RunClients(flag.Args())
}

func RunClients(torrentFilePaths []string) {
	var torrentClientWaitGroup sync.WaitGroup
	for _, path := range torrentFilePaths {
		go func() {
			defer torrentClientWaitGroup.Done()
			NewTorrentClient(path).Run()
		}()
		torrentClientWaitGroup.Add(1)
	}
	torrentClientWaitGroup.Wait()
}

// Notable extensions to the bittorrent protocol are listed here
// http://en.wikipedia.org/wiki/Torrent_file

type TorrentClient struct {
	TorrentFilePath string
	PeerID          string
	Bencoded        string
	Bdecoded        map[string]interface{}
	Port            int
}

func NewTorrentClient(torrentFilePath string) *TorrentClient {
	bencoded, err := ioutil.ReadFile(torrentFilePath)
	check(err)
	bdecoded, err := bencode.Decode(strings.NewReader(string(bencoded)))
	check(err)

	return &TorrentClient{
		TorrentFilePath: torrentFilePath,
		PeerID:          MakePeerID(),
		Bencoded:        string(bencoded),
		Bdecoded:        bdecoded.(map[string]interface{}),
		Port:            6881, // TODO set sensible value here
	}
}

func (c *TorrentClient) Run() {
	var peerWaitGroup sync.WaitGroup
	for _, announceUrl := range c.AnnounceUrls() {
		go func(announceUrl string) {
			defer peerWaitGroup.Done()
			c.GetPeers(announceUrl)
		}(announceUrl)
		peerWaitGroup.Add(1)
	}
	peerWaitGroup.Wait()
}

func (c *TorrentClient) AnnounceUrl() string {
	return c.AnnounceUrls()[0]
}

func (c *TorrentClient) AnnounceUrls() []string {
	// http://www.bittorrent.org/beps/bep_0012.html
	// Note that we do not implement the full specification : all trackers will
	// be shuffled and queried.
	var urls []string

	if announceUrlsValue, isPresent := c.Bdecoded["announce-list"]; isPresent {
		for _, announceUrlsArr := range announceUrlsValue.([]interface{}) {
			for _, announceUrls := range announceUrlsArr.([]interface{}) {
				urls = append(urls, announceUrls.(string))
			}
		}
	} else if announceUrl, isPresent := c.Bdecoded["announce"]; isPresent {
		urls = append(urls, announceUrl.(string))
	}
	return urls
}

func (c *TorrentClient) BdecodedInfo() map[string]interface{} {
	return c.Bdecoded["info"].(map[string]interface{})
}

func (c *TorrentClient) InfoHash() string {
	var infoBuffer bytes.Buffer
	bencode.Marshal(&infoBuffer, c.BdecodedInfo())
	var infohash [20]byte = sha1.Sum(infoBuffer.Bytes())
	return string(infohash[:])
}

func (c *TorrentClient) GetPeers(announceUrl string) []Peer {
	var peers []Peer
	if strings.HasPrefix(announceUrl, "udp") {
	} else if strings.HasPrefix(announceUrl, "http") {
		params := url.Values{}
		params.Set("info_hash", c.InfoHash())
		params.Set("peer_id", c.PeerID)
		params.Set("port", strconv.Itoa(c.Port))
		params.Set("uploaded", "0")    // TODO
		params.Set("downloaded", "0")  // TODO
		params.Set("left", "0")        // TODO
		params.Set("event", "started") // TODO
		response, err := HttpGetBdecoded(announceUrl, &params)
		if err != nil {
			//fmt.Println("###############", u.String(), err)
			return peers
		} else {
			if _, requestFailed := response["failure reason"]; requestFailed {
				// Failure reason is present in response[failure reason]
				//fmt.Println("***************", failureReason)
			} else {
				// TODO Compact representation?
				// http://www.bittorrent.org/beps/bep_0023.html
				//fmt.Println("---------------", len(body), string(body))
				encodedPeers := response["peers"].(string)
				peers := DecodePeers(encodedPeers)
				fmt.Println("+++++++++++++++", peers)
			}
		}
	}
	return peers
}

func DecodePeers(encodedPeers string) []Peer {
	var peers []Peer
	for pos := 0; pos < len(encodedPeers); pos += 6 {
		ip := encodedPeers[pos : pos+4]
		port := encodedPeers[pos+4 : pos+6]
		peers = append(peers, Peer{
			IP: strconv.Itoa(int(ip[0])) + "." +
				strconv.Itoa(int(ip[1])) + "." +
				strconv.Itoa(int(ip[2])) + "." +
				strconv.Itoa(int(ip[3])),
			Port: int(port[0])*255 + int(port[1]),
		})
	}
	return peers
}

func HttpGetBdecoded(uri string, params *url.Values) (map[string]interface{}, error) {
	response, err := HttpGet(uri, params)
	if err != nil {
		return map[string]interface{}{}, err
	}
	bdecodedResponseRaw, err := bencode.Decode(strings.NewReader(response))
	if err != nil {
		return map[string]interface{}{}, err
	}
	return bdecodedResponseRaw.(map[string]interface{}), nil
}

func HttpGet(uri string, params *url.Values) (string, error) {
	// Build full url
	urlFull, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	urlFull.RawQuery = params.Encode()

	// Make query
	response, err := http.Get(urlFull.String())
	if err != nil {
		return "", err
	}

	// Parse response
	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	return string(body), err
}

type Peer struct {
	PeerID string
	IP     string
	Port   int
}

func MakePeerID() string {
	letters := "abcdefghijklmnopqrstuvwxyz0123456789"
	var peerID [20]byte
	for i := 0; i < 20; i++ {
		peerID[i] = letters[rand.Intn(len(letters))]
	}
	return string(peerID[:])
}

func check(err error) {
	if err != nil {
		fmt.Println("## ERROR ", err)
		panic(err)
	}
}
