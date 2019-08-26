// Package redis for cache provider
//
// depend on github.com/gomodule/redigo/redis
//
// go install github.com/gomodule/redigo/redis
//
// Usage:
// import(
//   _ "github.com/astaxie/beego/cache/redis"
//   "github.com/astaxie/beego/cache"
// )
//
//  bm, err := cache.NewCache("redis-sentinel", `{"Addrs": ":26379,:26379,172.16.38.233:26379","MasterName":"mymaster","dbNum":"0","Auth":"@Anjieych@"}`)
//
//  more docs http://beego.me/docs/module/cache.md
package redisSentinelCache

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/FZambia/sentinel"
	"github.com/astaxie/beego/cache"
	"github.com/gomodule/redigo/redis"
)

var (
	// DefaultKey the collection name of redis for cache adapter.
	DefaultKey = "beecacheRedis"
)

// Cache is Redis cache adapter.
type Cache struct {
	p          *redis.Pool // redis connection pool
	conninfo   string
	dbNum      int
	key        string
	password   string
	masterName string
}

// NewRedisSentinelCache create new sentinel redis cache with default collection name.
func NewRedisSentinelCache() cache.Cache {
	return &Cache{key: DefaultKey}
}

// actually do the redis cmds
func (rc *Cache) do(commandName string, args ...interface{}) (reply interface{}, err error) {
	c := rc.p.Get()
	defer c.Close()

	return c.Do(commandName, args...)
}

// Get cache from redis.
func (rc *Cache) Get(key string) interface{} {
	if v, err := rc.do("GET", key); err == nil {
		return v
	}
	return nil
}

// GetMulti get cache from redis.
func (rc *Cache) GetMulti(keys []string) []interface{} {
	size := len(keys)
	var rv []interface{}
	c := rc.p.Get()
	defer c.Close()
	var err error
	for _, key := range keys {
		err = c.Send("GET", key)
		if err != nil {
			goto ERROR
		}
	}
	if err = c.Flush(); err != nil {
		goto ERROR
	}
	for i := 0; i < size; i++ {
		if v, err := c.Receive(); err == nil {
			rv = append(rv, v.([]byte))
		} else {
			rv = append(rv, err)
		}
	}
	return rv
ERROR:
	rv = rv[0:0]
	for i := 0; i < size; i++ {
		rv = append(rv, nil)
	}

	return rv
}

// Put put cache to redis.
func (rc *Cache) Put(key string, val interface{}, timeout time.Duration) error {
	var err error
	if _, err = rc.do("SETEX", key, int64(timeout/time.Second), val); err != nil {
		return err
	}

	if _, err = rc.do("HSET", rc.key, key, true); err != nil {
		return err
	}
	return err
}

// Delete delete cache in redis.
func (rc *Cache) Delete(key string) error {
	var err error
	if _, err = rc.do("DEL", key); err != nil {
		return err
	}
	_, err = rc.do("HDEL", rc.key, key)
	return err
}

// IsExist check cache's existence in redis.
func (rc *Cache) IsExist(key string) bool {
	v, err := redis.Bool(rc.do("EXISTS", key))
	if err != nil {
		return false
	}
	if v == false {
		if _, err = rc.do("HDEL", rc.key, key); err != nil {
			return false
		}
	}
	return v
}

// Incr increase counter in redis.
func (rc *Cache) Incr(key string) error {
	_, err := redis.Bool(rc.do("INCRBY", key, 1))
	return err
}

// Decr decrease counter in redis.
func (rc *Cache) Decr(key string) error {
	_, err := redis.Bool(rc.do("INCRBY", key, -1))
	return err
}

// ClearAll clean all cache in redis. delete this redis collection.
func (rc *Cache) ClearAll() error {
	cachedKeys, err := redis.Strings(rc.do("HKEYS", rc.key))
	if err != nil {
		return err
	}
	for _, str := range cachedKeys {
		if _, err = rc.do("DEL", str); err != nil {
			return err
		}
	}
	_, err = rc.do("DEL", rc.key)
	return err
}

// StartAndGC start redis cache adapter.
// config is like {"key":"collection key","conn":"connection info","dbNum":"0"}
// the cache item in redis are stored forever,
// so no gc operation.
func (rc *Cache) StartAndGC(config string) error {
	var cf map[string]string
	json.Unmarshal([]byte(config), &cf)

	if _, ok := cf["key"]; !ok {
		cf["key"] = DefaultKey
	}
	if _, ok := cf["Addrs"]; !ok {
		return errors.New("config has no Addrs key")
	}
	if _, ok := cf["dbNum"]; !ok {
		cf["dbNum"] = "0"
	}
	if _, ok := cf["Auth"]; !ok {
		cf["Auth"] = ""
	}
	if _, ok := cf["MasterName"]; !ok {
		cf["MasterName"] = "mymaster"
	}

	rc.key = cf["key"]
	rc.conninfo = cf["Addrs"]
	rc.dbNum, _ = strconv.Atoi(cf["dbNum"])
	rc.password = cf["Auth"]
	rc.masterName = cf["MasterName"]

	rc.connectInit()

	c := rc.p.Get()
	defer c.Close()

	return c.Err()
}

// connect to redis.
func (rc *Cache) connectInit() {

	sntnl := &sentinel.Sentinel{
		Addrs:      strings.Split(rc.conninfo, ","),
		MasterName: rc.masterName,
		Dial: func(addr string) (redis.Conn, error) {
			timeout := 500 * time.Millisecond
			c, err := redis.DialTimeout("tcp", addr, timeout, timeout, timeout)
			if err != nil {
				return nil, err
			}
			return c, nil
		},
	}

	// initialize a new pool
	rc.p = &redis.Pool{
		MaxIdle:     3,
		MaxActive:   64,
		Wait:        true,
		IdleTimeout: 300 * time.Second,
		Dial: func() (redis.Conn, error) {
			masterAddr, err := sntnl.MasterAddr()
			if err != nil {
				return nil, err
			}
			c, err := redis.Dial("tcp", masterAddr)
			if err != nil {
				return nil, err
			}

			if rc.password != "" {
				if _, err := c.Do("AUTH", rc.password); err != nil {
					c.Close()
					return nil, err
				}
			}

			_, selecterr := c.Do("SELECT", rc.dbNum)
			if selecterr != nil {
				c.Close()
				return nil, selecterr
			}

			return c, nil
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			if !sentinel.TestRole(c, "master") {
				return errors.New("Role check failed")
			} else {
				return nil
			}
		},
	}

}

func init() {
	cache.Register("redis-sentinel", NewRedisSentinelCache)
}
