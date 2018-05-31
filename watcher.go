package rediswatcher

import (
	"runtime"

	"fmt"
	"github.com/casbin/casbin/persist"
	"github.com/garyburd/redigo/redis"
)

type Watcher struct {
	options    WatcherOptions
	connection redis.Conn
	callback   func(string)
}

// NewWatcher creates a new Watcher to be used with a Casbin enforcer
// addr is a redis target string in the format "host:port"
// setters allows for inline WatcherOptions
//
// 		Example:
// 				w, err := rediswatcher.NewWatcher("127.0.0.1:6379", rediswatcher.Password("pass"), rediswatcher.Channel("/yourchan"))
//
// A custom redis.Conn can be provided to NewWatcher
//
// 		Example:
// 				c, err := redis.Dial("tcp", ":6379")
// 				w, err := rediswatcher.NewWatcher("", rediswatcher.WithRedisConnection(c)
//
func NewWatcher(addr string, setters ...WatcherOption) (persist.Watcher, error) {
	w := &Watcher{}

	w.options = WatcherOptions{
		Channel:  "/casbin",
		Protocol: "tcp",
	}

	for _, setter := range setters {
		setter(&w.options)
	}

	if err := w.connect(addr); err != nil {
		return nil, err
	}

	// call destructor when the object is released
	runtime.SetFinalizer(w, finalizer)

	go func() {
		for {
			err := w.subscribe()
			if err != nil {
				fmt.Printf("Failure from Redis subscription: %v", err)
			}
		}
	}()

	return w, nil
}

// SetUpdateCallBack sets the update callback function invoked by the watcher
// when the policy is updated. Defaults to Enforcer.LoadPolicy()
func (w *Watcher) SetUpdateCallback(callback func(string)) error {
	w.callback = callback
	return nil
}

// Update publishes a message to all other casbin instances telling them to
// invoke their update callback
func (w *Watcher) Update() error {
	if _, err := w.connection.Do("PUBLISH", w.options.Channel, "casbin rules updated"); err != nil {
		return err
	}

	return nil
}

func (w *Watcher) connect(addr string) error {
	if w.options.Connection != nil {
		w.connection = w.options.Connection
		return nil
	}

	c, err := redis.Dial(w.options.Protocol, addr)
	if err != nil {
		return err
	}

	if w.options.Password != "" {
		_, err := c.Do("AUTH", w.options.Password)
		if err != nil {
			c.Close()
			return err
		}
	}

	w.connection = c
	return nil
}

func (w *Watcher) subscribe() error {
	psc := redis.PubSubConn{Conn: w.connection}
	if err := psc.Subscribe(w.options.Channel); err != nil {
		return err
	}
	defer psc.Unsubscribe()

	for {
		switch n := psc.Receive().(type) {
		case error:
			return n
		case redis.Message:
			if w.callback != nil {
				w.callback(string(n.Data))
			}
		case redis.Subscription:
			if n.Count == 0 {
				return nil
			}
		}
	}

	return nil
}

func finalizer(w *Watcher) {
	w.connection.Close()
}