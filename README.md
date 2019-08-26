##What

redisSentinelCache is redis cache privoder for github.com/astaxie/beego/cache with redis sentinel.

##How

```golang
import (
	"github.com/astaxie/beego/cache"
	"github.com/garyburd/redigo/redis"
)

  bm, err := cache.NewCache("redis-sentinel", `{"Addrs": ":26379,:26379,172.16.38.233:26379","MasterName":"mymaster","dbNum":"0","Auth":"@Anjieych@"}`)
    if err != nil {
		t.Error("init err")
	}
	timeoutDuration := 10 * time.Second
	if err = bm.Put("astaxie", 1, timeoutDuration); err != nil {
		t.Error("set Error", err)
	}
	if !bm.IsExist("astaxie") {
		t.Error("check err")
	} }
```
