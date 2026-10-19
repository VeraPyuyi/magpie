package dotbridge

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "strings"
 "testing"
 "time"

 "github.com/yetone/magpie/internal/edit"
)

func identityFixture(t *testing.T, handler http.Handler) (*Connection, Config) {
 t.Helper()
 t.Setenv("XDG_CONFIG_HOME", t.TempDir())
 SetCredentialStore(newMemStore())
 t.Cleanup(func(){ SetCredentialStore(newMemStore()) })
 srv := httptest.NewServer(handler)
 t.Cleanup(srv.Close)
 cfg := Config{Enabled:true, Address:strings.TrimPrefix(srv.URL,"http://"), Credential:"same-client"}
 if err := SavePairing(cfg,"secret-A"); err != nil { t.Fatal(err) }
 cfg, err := LoadConfig(); if err != nil { t.Fatal(err) }
 c, err := PairedConnection(); if err != nil { t.Fatal(err) }
 return c,cfg
}

func identityReadyHandler() http.Handler {
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
 now := time.Now().UTC()
 _ = json.NewEncoder(w).Encode(map[string]any{"version":1,"enabled":true,"paired":true,"ready":true,"verified_at":now.Add(-time.Second),"expires_at":now.Add(time.Minute),"subscription":map[string]any{"state":"active","expires_at":now.Add(time.Minute)},"capabilities":map[string]bool{"text":true}})
 })
}

func TestDotIdentityExternalReplacementInvalidatesReady(t *testing.T) {
 c,cfg := identityFixture(t,identityReadyHandler())
 if _,err := c.Status(context.Background()); err != nil { t.Fatal(err) }
 if !CachedReady(time.Now()) { t.Fatal("A should be ready") }
 // A separate process's atomic config write bypasses every in-process setter.
 cfg.Credential = "other-client"
 b,_ := json.Marshal(cfg)
 if err := edit.WriteAtomic(configPath(),b); err != nil { t.Fatal(err) }
 if CachedReady(time.Now()) { t.Fatal("B inherited A readiness after external config write") }
}

func TestDotIdentityInflightOldStatusCannotPublish(t *testing.T) {
 entered,release := make(chan struct{}),make(chan struct{})
 handler := http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){ close(entered); <-release; identityReadyHandler().ServeHTTP(w,r) })
 c,cfg := identityFixture(t,handler)
 done := make(chan error,1)
 go func(){ _,err := c.Status(context.Background()); done<-err }()
 <-entered
 cfg.Credential="other-client"
 b,_ := json.Marshal(cfg)
 err := edit.WriteAtomic(configPath(),b)
 close(release)
 if err != nil { t.Fatal(err) }
 <-done // An obsolete connection may also reject its response after the switch.
 if CachedReady(time.Now()) { t.Fatal("inflight A published readiness for B") }
}

func TestDotIdentityUnboundStatusCannotPublish(t *testing.T) {
 c,_ := identityFixture(t,identityReadyHandler())
 unbound,err := NewConnection(strings.TrimPrefix(c.baseURL,"http://"),func()(string,error){ return "secret-A",nil })
 if err != nil { t.Fatal(err) }
 if _,err := unbound.Status(context.Background()); err != nil { t.Fatal(err) }
 if CachedReady(time.Now()) { t.Fatal("unbound NewConnection published production readiness") }
}

func TestDotIdentityDistinctGenerationSameAddressCredential(t *testing.T) {
 t.Setenv("XDG_CONFIG_HOME", t.TempDir())
 cfg := Config{Enabled: true, Address: "127.0.0.1:9", Credential: "same-ref"}
 if err := SavePairing(cfg, "token-1"); err != nil { t.Fatal(err) }
 a, err := LoadConfig(); if err != nil { t.Fatal(err) }
 idA := PairingIdentity(a)
 if err := SavePairing(a, "token-2"); err != nil { t.Fatal(err) }
 b, err := LoadConfig(); if err != nil { t.Fatal(err) }
 idB := PairingIdentity(b)
 if idA == "" || idB == "" || idA == idB || a.PairingGeneration == b.PairingGeneration {
  t.Fatalf("generation must rotate identity: A=%s B=%s genA=%s genB=%s", idA, idB, a.PairingGeneration, b.PairingGeneration)
 }
}

func TestDotIdentityOffReenableCannotRestoreReady(t *testing.T) {
 c,cfg := identityFixture(t,identityReadyHandler())
 if _,err := c.Status(context.Background()); err != nil { t.Fatal(err) }
 cfg.Enabled=false
 if err := SaveConfig(cfg); err != nil { t.Fatal(err) }
 cfg.Enabled=true
 if err := SaveConfig(cfg); err != nil { t.Fatal(err) }
 if CachedReady(time.Now()) { t.Fatal("off/re-enable restored A readiness") }
 b,err := os.ReadFile(configPath()); if err != nil { t.Fatal(err) }
 if strings.Contains(string(b),"secret-A") { t.Fatal("secret in public config") }
}
