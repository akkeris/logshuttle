package storage

import (
	. "github.com/smartystreets/goconvey/convey"
	"testing"
	"time"
	"os"
	"strings"
)

func CreateMemoryStorage() (*Storage) {
	var store MemoryStorage
	store.Init("")
	var s Storage = &store
	return &s
}

func CreatePostgresStorage() (*Storage) {
	var store PostgresStorage
	store.Init(os.Getenv("POSTGRES_URL"))
	var s Storage = &store
	return &s
}

func CreateRedisStorage() (*Storage) {
	var store RedisStorage
	store.Init(strings.Replace(os.Getenv("REDIS_URL"), "redis://", "", 1))
	var s Storage = &store
	return &s
}

func TestShuttle(t *testing.T) {
	Convey("MemoryStorage: Ensure we can add a route to memory storage.", t, func() {
		memstr := CreateMemoryStorage()
		created := time.Now()
		err := (*memstr).AddRoute(Route{Id:"Test", Space:"space", App:"app", Created:created, Updated:created, DestinationUrl:"somewhere"})
		So(err, ShouldEqual, nil)
		routes, err := (*memstr).GetRoutes()
		So(err, ShouldEqual, nil)
		So(len(routes), ShouldEqual, 1)
		So(routes[0].Id, ShouldEqual, "Test")
		So(routes[0].Space, ShouldEqual, "space")
		So(routes[0].App, ShouldEqual, "app")
		So(routes[0].Created, ShouldEqual, created)
		So(routes[0].Updated, ShouldEqual, created)
		So(routes[0].DestinationUrl, ShouldEqual, "somewhere")
	})
	Convey("MemoryStorage: Ensure we can add multiple routes to memory storage.", t, func() {
		memstr := CreateMemoryStorage()
		routes_to_add := []Route{ Route{Id:"Test", Space:"space", App:"app", Created:time.Now(), Updated:time.Now(), DestinationUrl:"somewhere"}, Route{Id:"Test2", Space:"space2", App:"app2", Created:time.Now(), Updated:time.Now(), DestinationUrl:"somewhere2"} }
		err := (*memstr).AddRoutes(routes_to_add)
		So(err, ShouldEqual, nil)
		routes, err := (*memstr).GetRoutes()
		So(err, ShouldEqual, nil)
		So(len(routes), ShouldEqual, 2)
		So(routes[0].Id, ShouldEqual, "Test")
		So(routes[1].Id, ShouldEqual, "Test2")
	})
	Convey("MemoryStorage: Ensure we can remove routes.", t, func() {
		memstr := CreateMemoryStorage()
		routes_to_add := []Route{ Route{Id:"Test", Space:"space", App:"app", Created:time.Now(), Updated:time.Now(), DestinationUrl:"somewhere"}, Route{Id:"Test2", Space:"space2", App:"app2", Created:time.Now(), Updated:time.Now(), DestinationUrl:"somewhere2"} }
		err := (*memstr).AddRoutes(routes_to_add)
		So(err, ShouldEqual, nil)
		routes, err := (*memstr).GetRoutes()
		So(err, ShouldEqual, nil)
		So(len(routes), ShouldEqual, 2)
		So(routes[0].Id, ShouldEqual, "Test")
		So(routes[1].Id, ShouldEqual, "Test2")
		err = (*memstr).RemoveRoute(routes_to_add[1])
		So(err, ShouldEqual, nil)
		routes, err = (*memstr).GetRoutes()
		So(err, ShouldEqual, nil)
		So(len(routes), ShouldEqual, 1)
		So(routes[0].Id, ShouldEqual, "Test")
	})
	Convey("MemoryStorage: Ensure we can get a route by id.", t, func() {
		memstr := CreateMemoryStorage()
		err := (*memstr).AddRoute(Route{Id:"Test", Space:"space", App:"app", Created:time.Now(), Updated:time.Now(), DestinationUrl:"somewhere"})
		So(err, ShouldEqual, nil)
		route, err := (*memstr).GetRouteById("Test")
		So(err, ShouldEqual, nil)
		So(route.Id, ShouldEqual, "Test")
	})
	Convey("MemoryStorage: Ensure we can set a session.", t, func() {
		memstr := CreateMemoryStorage()
		err := (*memstr).SetSession("MySession", LogSession{App:"app", Space:"space", Lines:1, Tail:true}, time.Second * 15)
		So(err, ShouldEqual, nil)
		session, err := (*memstr).GetSession("MySession")
		So(err, ShouldEqual, nil)
		So(session.App, ShouldEqual, "app")
		So(session.Space, ShouldEqual, "space")
		So(session.Lines, ShouldEqual, 1)
		So(session.Tail, ShouldEqual, true)
	})
	Convey("MemoryStorage: Ensure sessions expire.", t, func() {
		memstr := CreateMemoryStorage()
		err := (*memstr).SetSession("MySession", LogSession{App:"app", Space:"space", Lines:1, Tail:true}, time.Second * 1)
		So(err, ShouldEqual, nil)
		t := time.NewTicker(time.Second * 2)
		<-t.C
		_, err = (*memstr).GetSession("MySession")
		So(err, ShouldNotEqual, nil)
	})
	Convey("MemoryStorage: Ensure health check works.", t, func() {
		memstr := CreateMemoryStorage()
		So((*memstr).HealthCheck(), ShouldEqual, nil)
	})
	if os.Getenv("POSTGRES_URL") != "" {
		Convey("PostgresStorage: Ensure we can add a route to memory storage.", t, func() {
			memstr := CreatePostgresStorage()
			created := time.Now()
			err := (*memstr).AddRoute(Route{Id:"Test", Space:"space", App:"app", Created:created, Updated:created, DestinationUrl:"somewhere"})
			So(err, ShouldEqual, nil)
			routes, err := (*memstr).GetRoutes()
			So(err, ShouldEqual, nil)
			So(len(routes), ShouldEqual, 1)
			So(routes[0].Id, ShouldEqual, "Test")
			So(routes[0].Space, ShouldEqual, "space")
			So(routes[0].App, ShouldEqual, "app")
			So(routes[0].Created, ShouldHappenOnOrBefore, created)
			So(routes[0].Updated, ShouldHappenOnOrBefore, created)
			So(routes[0].DestinationUrl, ShouldEqual, "somewhere")
		})
		Convey("PostgresStorage: Ensure we can add multiple routes to memory storage.", t, func() {
			memstr := CreatePostgresStorage()
			routes_to_add := []Route{ Route{Id:"Test", Space:"space", App:"app", Created:time.Now(), Updated:time.Now(), DestinationUrl:"somewhere"}, Route{Id:"Test2", Space:"space2", App:"app2", Created:time.Now(), Updated:time.Now(), DestinationUrl:"somewhere2"} }
			err := (*memstr).AddRoutes(routes_to_add)
			So(err, ShouldEqual, nil)
			routes, err := (*memstr).GetRoutes()
			So(err, ShouldEqual, nil)
			So(len(routes), ShouldEqual, 2)
			So(routes[0].Id, ShouldEqual, "Test")
			So(routes[1].Id, ShouldEqual, "Test2")
		})
		Convey("PostgresStorage: Ensure we can remove routes.", t, func() {
			memstr := CreatePostgresStorage()
			routes_to_add := []Route{ Route{Id:"Test", Space:"space", App:"app", Created:time.Now(), Updated:time.Now(), DestinationUrl:"somewhere"}, Route{Id:"Test2", Space:"space2", App:"app2", Created:time.Now(), Updated:time.Now(), DestinationUrl:"somewhere2"} }
			err := (*memstr).AddRoutes(routes_to_add)
			So(err, ShouldEqual, nil)
			routes, err := (*memstr).GetRoutes()
			So(err, ShouldEqual, nil)
			So(len(routes), ShouldEqual, 2)
			So(routes[0].Id, ShouldEqual, "Test")
			So(routes[1].Id, ShouldEqual, "Test2")
			err = (*memstr).RemoveRoute(routes_to_add[1])
			So(err, ShouldEqual, nil)
			routes, err = (*memstr).GetRoutes()
			So(err, ShouldEqual, nil)
			So(len(routes), ShouldEqual, 1)
			So(routes[0].Id, ShouldEqual, "Test")
		})
		Convey("PostgresStorage: Ensure we can get a route by id.", t, func() {
			memstr := CreatePostgresStorage()
			err := (*memstr).AddRoute(Route{Id:"Test", Space:"space", App:"app", Created:time.Now(), Updated:time.Now(), DestinationUrl:"somewhere"})
			So(err, ShouldEqual, nil)
			route, err := (*memstr).GetRouteById("Test")
			So(err, ShouldEqual, nil)
			So(route.Id, ShouldEqual, "Test")
		})
		Convey("PostgresStorage: Ensure we can set a session.", t, func() {
			memstr := CreatePostgresStorage()
			err := (*memstr).SetSession("MySession", LogSession{App:"app", Space:"space", Lines:1, Tail:true}, time.Second * 15)
			So(err, ShouldEqual, nil)
			session, err := (*memstr).GetSession("MySession")
			So(err, ShouldEqual, nil)
			So(session.App, ShouldEqual, "app")
			So(session.Space, ShouldEqual, "space")
			So(session.Lines, ShouldEqual, 1)
			So(session.Tail, ShouldEqual, true)
		})
		Convey("PostgresStorage: Ensure sessions expire.", t, func() {
			memstr := CreatePostgresStorage()
			err := (*memstr).SetSession("MySession2", LogSession{App:"app", Space:"space", Lines:1, Tail:true}, time.Second * 1)
			So(err, ShouldEqual, nil)
			t := time.NewTicker(time.Second * 2)
			<-t.C
			_, err = (*memstr).GetSession("MySession2")
			So(err, ShouldNotEqual, nil)
		})
		Convey("PostgresStorage: Ensure health check works.", t, func() {
			memstr := CreatePostgresStorage()
			So((*memstr).HealthCheck(), ShouldEqual, nil)
		})
	}
	if os.Getenv("REDIS_URL") != "" {
		Convey("RedisStorage: Ensure we can add a route to memory storage.", t, func() {
			memstr := CreateRedisStorage()
			created := time.Now()
			err := (*memstr).AddRoute(Route{Id:"Test", Space:"space", App:"app", Created:created, Updated:created, DestinationUrl:"somewhere"})
			So(err, ShouldEqual, nil)
			routes, err := (*memstr).GetRoutes()
			So(err, ShouldEqual, nil)
			So(len(routes), ShouldBeGreaterThan, 1)
		})
		Convey("RedisStorage: Ensure we can add multiple routes to memory storage.", t, func() {
			memstr := CreateRedisStorage()
			routes_before, err := (*memstr).GetRoutes()
			So(err, ShouldEqual, nil)
			routes_to_add := []Route{ Route{Id:"Test", Space:"space", App:"app", Created:time.Now(), Updated:time.Now(), DestinationUrl:"somewhere"}, Route{Id:"Test2", Space:"space2", App:"app2", Created:time.Now(), Updated:time.Now(), DestinationUrl:"somewhere2"} }
			err = (*memstr).AddRoutes(routes_to_add)
			So(err, ShouldEqual, nil)
			routes, err := (*memstr).GetRoutes()
			So(err, ShouldEqual, nil)
			So(len(routes) - len(routes_before), ShouldEqual, 2)
			var found1 = false
			var found2 = false
			for _, x := range routes {
				if x.Id == "Test" {
					found1 = true
				}
				if x.Id == "Test2" {
					found2 = true
				}
			}
			So(found1, ShouldEqual, true)
			So(found2, ShouldEqual, true)
		})
		Convey("RedisStorage: Ensure we can remove routes.", t, func() {
			memstr := CreateRedisStorage()
			routes_before, err := (*memstr).GetRoutes()
			So(err, ShouldEqual, nil)
			routes_to_add := []Route{ Route{Id:"Test", Space:"space", App:"app", Created:time.Now(), Updated:time.Now(), DestinationUrl:"somewhere"}, Route{Id:"Test2", Space:"space2", App:"app2", Created:time.Now(), Updated:time.Now(), DestinationUrl:"somewhere2"} }
			err = (*memstr).AddRoutes(routes_to_add)
			So(err, ShouldEqual, nil)
			routes, err := (*memstr).GetRoutes()
			So(err, ShouldEqual, nil)
			So(len(routes) - len(routes_before), ShouldEqual, 2)
			err = (*memstr).RemoveRoute(routes_to_add[1])
			So(err, ShouldEqual, nil)
			routes_after, err := (*memstr).GetRoutes()
			So(err, ShouldEqual, nil)
			So(len(routes) - len(routes_after), ShouldEqual, 1)
		})
		Convey("RedisStorage: Ensure we can get a route by id.", t, func() {
			memstr := CreateRedisStorage()
			created := time.Now()
			err := (*memstr).AddRoute(Route{Id:"Test", Space:"space", App:"app", Created:created, Updated:created, DestinationUrl:"somewhere"})
			So(err, ShouldEqual, nil)
			route, err := (*memstr).GetRouteById("Test")
			So(err, ShouldEqual, nil)
			So(route.Id, ShouldEqual, "Test")
			So(route.Space, ShouldEqual, "space")
			So(route.App, ShouldEqual, "app")
			So(route.Created, ShouldHappenOnOrBefore, created)
			So(route.Updated, ShouldHappenOnOrBefore, created)
			So(route.DestinationUrl, ShouldEqual, "somewhere")
		})
		Convey("RedisStorage: Ensure we can set a session.", t, func() {
			memstr := CreateRedisStorage()
			err := (*memstr).SetSession("MySession", LogSession{App:"app", Space:"space", Lines:1, Tail:true}, time.Second * 15)
			So(err, ShouldEqual, nil)
			session, err := (*memstr).GetSession("MySession")
			So(err, ShouldEqual, nil)
			So(session.App, ShouldEqual, "app")
			So(session.Space, ShouldEqual, "space")
			So(session.Lines, ShouldEqual, 1)
			So(session.Tail, ShouldEqual, true)
		})
		Convey("RedisStorage: Ensure sessions expire.", t, func() {
			memstr := CreateRedisStorage()
			err := (*memstr).SetSession("MySession2", LogSession{App:"app", Space:"space", Lines:1, Tail:true}, time.Second * 1)
			So(err, ShouldEqual, nil)
			t := time.NewTicker(time.Second * 2)
			<-t.C
			_, err = (*memstr).GetSession("MySession2")
			So(err, ShouldNotEqual, nil)
		})
		Convey("RedisStorage: Ensure health check works.", t, func() {
			memstr := CreateRedisStorage()
			So((*memstr).HealthCheck(), ShouldEqual, nil)
		})
	}
}
