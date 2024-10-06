package common

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/uuid"
)

func TestMapOfMaps(t *testing.T) {
	t.Parallel()
	mapOfMaps := NewMapOfMaps()
	mapOfMaps.Put("pkey1", "dev.a.global1", "127.0.10.1")
	mapOfMaps.Put("pkey1", "dev.a.global2", "127.0.10.2")
	mapOfMaps.Put("pkey2", "qa.a.global", "127.0.10.1")
	mapOfMaps.Put("pkey3", "stage.a.global", "127.0.10.1")

	map1 := mapOfMaps.Get("pkey1")
	if map1 == nil || map1.Get("dev.a.global1") != "127.0.10.1" {
		t.Fail()
	}

	map1.Delete("dev.a.global2")

	map12 := mapOfMaps.Get("pkey1")
	if map12.Get("dev.a.global2") != "" {
		t.Fail()
	}

	mapOfMaps.Put("pkey4", "prod.a.global", "127.0.10.1")

	map2 := mapOfMaps.Get("pkey4")
	if map2 == nil || map2.Get("prod.a.global") != "127.0.10.1" {
		t.Fail()
	}

	mapOfMaps.Put("pkey4", "prod.a.global", "127.0.10.1")

	mapOfMaps.Delete("pkey2")
	map3 := mapOfMaps.Get("pkey2")
	if map3 != nil {
		t.Fail()
	}

}

func TestDeleteMapOfMaps(t *testing.T) {
	t.Parallel()
	mapOfMaps := NewMapOfMaps()
	mapOfMaps.Put("pkey1", "dev.a.global1", "127.0.10.1")
	mapOfMaps.Put("pkey1", "dev.a.global2", "127.0.10.2")
	mapOfMaps.DeleteMap("pkey1", "dev.a.global1")

	mapValue := mapOfMaps.Get("pkey1")
	if len(mapValue.Get("dev.a.global1")) > 0 {
		t.Errorf("expected=nil, got=%v", mapValue.Get("dev.a.global1"))
	}
	if mapValue.Get("dev.a.global2") != "127.0.10.2" {
		t.Errorf("expected=%v, got=%v", "127.0.10.2", mapValue.Get("dev.a.global2"))
	}
}

func TestMapOfMapOfMaps(t *testing.T) {
	t.Parallel()
	mapOfMapOfMaps := NewMapOfMapOfMaps()
	mapOfMapOfMaps.Put("pkey1", "dev.a.global1", "127.0.10.1", "ns1")
	mapOfMapOfMaps.Put("pkey1", "dev.a.global2", "127.0.10.2", "ns2")
	mapOfMapOfMaps.Put("pkey2", "qa.a.global", "127.0.10.1", "ns3")
	mapOfMapOfMaps.Put("pkey2", "qa.a.global", "127.0.10.2", "ns4")

	mapOfMaps1 := mapOfMapOfMaps.Get("pkey1")
	if mapOfMaps1 == nil || mapOfMaps1.Get("dev.a.global1").Get("127.0.10.1") != "ns1" {
		t.Fail()
	}
	if mapOfMapOfMaps.Len() != 2 {
		t.Fail()
	}

	mapOfMaps1.Delete("dev.a.global2")

	mapOfMaps2 := mapOfMapOfMaps.Get("pkey1")
	if mapOfMaps2.Get("dev.a.global2") != nil {
		t.Fail()
	}

	keyList := mapOfMapOfMaps.Get("pkey2").Get("qa.a.global").GetKeys()
	if len(keyList) != 2 {
		t.Fail()
	}

	mapOfMapOfMaps.Put("pkey3", "prod.a.global", "127.0.10.1", "ns5")

	mapOfMaps3 := mapOfMapOfMaps.Get("pkey3")
	if mapOfMaps3 == nil || mapOfMaps3.Get("prod.a.global").Get("127.0.10.1") != "ns5" {
		t.Fail()
	}

	mapOfMaps4 := mapOfMapOfMaps.Get("pkey4")
	if mapOfMaps4 != nil {
		t.Fail()
	}

	mapOfMaps5 := NewMapOfMaps()
	mapOfMaps5.Put("dev.b.global", "ns6", "ns6")
	mapOfMapOfMaps.PutMapofMaps("pkey5", mapOfMaps5)
	if mapOfMapOfMaps.Get("pkey5") == nil || mapOfMapOfMaps.Get("pkey5").Get("dev.b.global").Get("ns6") != "ns6" {
		t.Fail()
	}

}

func TestAdmiralParams(t *testing.T) {
	admiralParams := AdmiralParams{SANPrefix: "custom.san.prefix"}
	admiralParamsStr := admiralParams.String()
	expectedContainsStr := "SANPrefix=custom.san.prefix"
	if !strings.Contains(admiralParamsStr, expectedContainsStr) {
		t.Errorf("AdmiralParams String doesn't have the expected Stringified value expected to contain %v", expectedContainsStr)
	}
}

func TestMapOfMapConcurrency(t *testing.T) {

	mapOfMaps := NewMapOfMaps()
	mapOfMaps.Put("pkey1", "dev.a.global2", "127.0.10.2")
	mapOfMaps.Put("pkey2", "qa.a.global", "127.0.10.1")
	mapOfMaps.Put("pkey3", "stage.a.global", "127.0.10.1")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				mapOfMaps.Put(string(uuid.NewUUID()), "test1", "value1")
			}
		}
	}(ctx)

	time.Sleep(1 * time.Second)

	mapOfMaps.Range(func(k string, v *Map) {
		assert.NotNil(t, k)
	})

}

func TestMapOfMapsRange(t *testing.T) {

	mapOfMaps := NewMapOfMaps()
	mapOfMaps.Put("pkey1", "dev.a.global2", "127.0.10.2")
	mapOfMaps.Put("pkey2", "qa.a.global", "127.0.10.1")
	mapOfMaps.Put("pkey3", "stage.a.global", "127.0.10.1")

	keys := make(map[string]string, len(mapOfMaps.cache))
	for _, k := range keys {
		keys[k] = k
	}

	numOfIter := 0
	mapOfMaps.Range(func(k string, v *Map) {
		assert.NotNil(t, keys[k])
		numOfIter++
	})

	assert.Equal(t, 3, numOfIter)

}

func TestMapConcurrency(t *testing.T) {

	m := NewMap()
	m.Put("pkey1", "127.0.10.2")
	m.Put("pkey2", "127.0.10.1")
	m.Put("pkey3", "127.0.10.1")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				m.Put(string(uuid.NewUUID()), "value1")
			}
		}
	}(ctx)

	time.Sleep(1 * time.Second)

	m.Range(func(k string, v string) {
		assert.NotNil(t, k)
	})

}

func TestMapRange(t *testing.T) {

	m := NewMap()
	m.Put("pkey1", "127.0.10.2")
	m.Put("pkey2", "127.0.10.1")
	m.Put("pkey3", "127.0.10.1")

	keys := make(map[string]string, len(m.cache))
	for _, k := range keys {
		keys[k] = k
	}

	numOfIter := 0
	m.Range(func(k string, v string) {
		assert.NotNil(t, keys[k])
		numOfIter++
	})

	assert.Equal(t, 3, numOfIter)

}

func TestSidecarEgressGet(t *testing.T) {

	egressMap := NewSidecarEgressMap()
	egressMap.Put("pkey1", "dkey1", "pkey2", "fqdn", map[string]string{"pkey2": "pkey2"})

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(3*time.Second))
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)
	// Producer go routine
	go func(ctx context.Context) {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				egressMap.Put("pkey1", "dkey1", string(uuid.NewUUID()), "fqdn", map[string]string{"pkey2": "pkey2"})
			}
		}
	}(ctx)

	// Consumer go routine
	go func(ctx context.Context) {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				assert.NotNil(t, egressMap.Get("pkey1"))
			}
		}
	}(ctx)

	wg.Wait()
}

func TestSidecarEgressRange(t *testing.T) {

	egressMap := NewSidecarEgressMap()
	egressMap.Put("pkey1", "dkey1", "pkey2", "fqdn", map[string]string{"pkey2": "pkey2"})
	egressMap.Put("pkey2", "dkey2", "pkey2", "fqdn", map[string]string{"pkey2": "pkey2"})
	egressMap.Put("pkey3", "dkey3", "pkey2", "fqdn", map[string]string{"pkey2": "pkey2"})

	numOfIter := 0
	egressMap.Range(func(k string, v map[string]map[string]SidecarEgress) {
		assert.NotNil(t, v)
		numOfIter++
	})

	assert.Equal(t, 3, numOfIter)

}
