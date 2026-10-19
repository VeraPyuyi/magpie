package gateway

import (
 "net/http/httptest"
 "testing"

 "github.com/yetone/magpie/internal/dotbridge"
 "github.com/yetone/magpie/internal/provider"
)

func TestDotPairingNamespacesStableRequestAndSession(t *testing.T) {
 enableDotBridgeForGateway(t)
 r := httptest.NewRequest("POST","/v1/chat/completions",nil)
 r.Header.Set(dotRequestHeader,"user-stable-id")
 r.Header.Set("X-Session-ID","user-session")
 body := []byte(`{"model":"dot/text","messages":[{"role":"user","content":"hello"}]}`)
 a,err := prepareDot(r,provider.Chat,body)
 if err != nil { t.Fatal(err) }
 scopeA := dotScope(a)
 idA := dotAttemptOf(a.Context()).Snapshot().RequestID
 subA := a.Context().Value(dotSubmissionKey{}).(dotbridge.Submission)
 if _,_,err := dotbridge.Reserve(subA,scopeA); err != nil { t.Fatal(err) }
 cfg,err := dotbridge.LoadConfig(); if err != nil { t.Fatal(err) }
 if err := dotbridge.SavePairing(cfg,"replacement-token"); err != nil { t.Fatal(err) }
 b,err := prepareDot(r,provider.Chat,body); if err != nil { t.Fatal(err) }
 subB := b.Context().Value(dotSubmissionKey{}).(dotbridge.Submission)
 if dotScope(b)==scopeA || dotAttemptOf(b.Context()).Snapshot().RequestID==idA || subA.SessionID==subB.SessionID { t.Fatal("new generation reused gateway identity") }
 old,err := dotbridge.Requests(scopeA); if err != nil || len(old)!=1 || !old[0].BlockReplay { t.Fatalf("old namespace lost: %+v %v",old,err) }
 current,err := dotbridge.Requests(dotScope(b)); if err != nil || len(current)!=0 { t.Fatalf("old record recovered as new pairing: %+v %v",current,err) }
 if dotScope(a)!=scopeA { t.Fatal("prepared request identity changed midflight") }
}
