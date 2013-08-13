package gibson

import (
	"encoding/json"
	"github.com/cloudfoundry/go_cfmessagebus"
	"log"
	"sync"
	"time"
)

type RouterClient interface {
	Greet() error
	Register(port int, uri string) error
	Unregister(port int, uri string) error
}

type CFRouterClient struct {
	Host       string
	messageBus cfmessagebus.MessageBus

	registry *Registry

	stopPeriodicCallback chan bool

	lock sync.RWMutex
}

type RegistryMessage struct {
	URIs []string `json:"uris"`
	Host string   `json:"host"`
	Port int      `json:"port"`
}

type RouterGreetingMessage struct {
	MinimumRegisterInterval int `json:"minimumRegisterIntervalInSeconds"`
}

func NewCFRouterClient(host string, messageBus cfmessagebus.MessageBus) *CFRouterClient {
	return &CFRouterClient{
		Host: host,

		registry: NewRegistry(),

		messageBus: messageBus,
	}
}

func (r *CFRouterClient) Greet() error {
	err := r.messageBus.Subscribe("router.start", r.handleGreeting)
	if err != nil {
		return err
	}

	return r.messageBus.Request("router.greet", []byte{}, r.handleGreeting)
}

func (r *CFRouterClient) Register(port int, uri string) error {
	r.registry.Register(port, uri)
	return r.sendRegistryMessage("router.register", port, []string{uri})
}

func (r *CFRouterClient) Unregister(port int, uri string) error {
	r.registry.Unregister(port, uri)
	return r.sendRegistryMessage("router.unregister", port, []string{uri})
}

func (r *CFRouterClient) handleGreeting(greeting []byte) {
	interval, err := r.intervalFrom(greeting)
	if err != nil {
		log.Printf("failed to parse router.start: %s\n", err)
		return
	}

	go r.callbackPeriodically(time.Duration(interval) * time.Second)
}

func (r *CFRouterClient) callbackPeriodically(interval time.Duration) {
	if r.stopPeriodicCallback != nil {
		r.stopPeriodicCallback <- true
	}

	cancel := make(chan bool)

	r.stopPeriodicCallback = cancel

	for stop := false; !stop; {
		select {
		case <-time.After(interval):
			r.registerAllRoutes()
		case stop = <-cancel:
		}
	}
}

func (r *CFRouterClient) sendRegistryMessage(subject string, port int, uris []string) error {
	msg := &RegistryMessage{
		URIs: uris,
		Host: r.Host,
		Port: port,
	}

	json, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return r.messageBus.Publish(subject, json)
}

func (r *CFRouterClient) intervalFrom(greetingPayload []byte) (int, error) {
	var greeting RouterGreetingMessage

	err := json.Unmarshal(greetingPayload, &greeting)
	if err != nil {
		return 0, err
	}

	return greeting.MinimumRegisterInterval, nil
}

func (r *CFRouterClient) registerAllRoutes() {
	r.registry.InParallel(func(port int, uris []string) {
		r.sendRegistryMessage("router.register", port, uris)
	})
}
