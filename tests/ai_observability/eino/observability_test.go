//go:build ai_observability && ai_business_agents

package eino

import (
 "compress/gzip"
 "context"
 "io"
 "net/http"
 "net/http/httptest"
 "strings"
 "sync"
 "testing"
 "time"

 "github.com/cloudwego/eino/schema"
 "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
 "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
 obs "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/observability"
 "go.opentelemetry.io/otel"
 collectorpb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
 tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
 "google.golang.org/protobuf/proto"
)

func TestOfficialCallbackStreamingHierarchyAndPrivacy(t *testing.T){
 var mu sync.Mutex;var spans []*tracepb.Span
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.URL.Path!="/api/public/otel/v1/traces"||r.Header.Get("x-langfuse-ingestion-version")!="4" {t.Error("invalid OTLP endpoint/header")}
  var reader io.Reader=r.Body
  if r.Header.Get("Content-Encoding")=="gzip"{z,err:=gzip.NewReader(r.Body);if err!=nil{t.Error(err);return};defer z.Close();reader=z}
  raw,_:=io.ReadAll(reader);var req collectorpb.ExportTraceServiceRequest
  if err:=proto.Unmarshal(raw,&req);err!=nil{t.Error(err);return}
  mu.Lock();defer mu.Unlock()
  for _,rs:=range req.ResourceSpans{for _,ss:=range rs.ScopeSpans{spans=append(spans,ss.Spans...)}}
  w.Header().Set("Content-Type","application/x-protobuf");w.WriteHeader(200)
 }));defer server.Close()
 global:=otel.GetTracerProvider()
 o,err:=NewObservation(context.Background(),config.LangfuseConfig{Enabled:true,Host:server.URL,PublicKey:"pk-test",SecretKey:"sk-test",UserHashSalt:"salt",SampleRate:1})
 if err!=nil{t.Fatal(err)}
 ctx,end:=o.Start(context.Background(),"Chat",7,"conv_trace","13812345678 123 Main Street sk-sensitive")
 runner,_,_,_:=newBusinessFlowRunner(t,map[string][]*schema.Message{
  supervisorAgentName:{businessCall("general_agent",`{"task":{"intent":"user_get","objective":"inspect profile"}}`),businessReply("final response")},
  "general_agent":{businessCall("user_get",`{}`),businessChildReply("profile read")},
 })
 events,err:=runner.Run(ctx,businessFlowRequest());if err!=nil{t.Fatal(err)}
 assertNoBusinessFlowErrors(t,events)
 final:="";for _,e:=range events{if e.Type==domain.EventAssistantMessage{final=e.Content}}
 if final!="final response"{t.Fatalf("stream consumed incorrectly: %q",final)}
 end(final)
 flush,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
 if err:=o.Shutdown(flush);err!=nil{t.Fatal(err)}
 if otel.GetTracerProvider()!=global{t.Fatal("global provider replaced")}
 mu.Lock();defer mu.Unlock()
 names:=map[string]bool{};types:=map[string]bool{}
 for _,s:=range spans{
  names[s.Name]=true
  if s.Name!="Chat"&&len(s.ParentSpanId)==0{t.Errorf("orphan span %s",s.Name)}
  for _,a:=range s.Attributes{if a.Key=="langfuse.observation.type"{types[a.Value.GetStringValue()]=true}}
  raw:=s.String();for _,secret:=range []string{"13812345678","Main Street","sk-sensitive"}{if strings.Contains(raw,secret){t.Fatalf("privacy leak: %s",secret)}}
 }
 if !names["supervisor_agent"]||!names["general_agent"]||!names["tool.execution"]||!types["generation"] {t.Fatalf("missing hierarchy: %v types %v",names,types)}
}

func TestObservationDisabledAndUnavailable(t *testing.T){
 o,err:=NewObservation(context.Background(),config.LangfuseConfig{});if err!=nil||o!=nil{t.Fatal("disabled config must be inert")}
 ctx,end:=o.Start(context.Background(),"Chat",7,"c","");end("");if obs.Active(ctx){t.Fatal("disabled observation activated")}
 o,err=NewObservation(context.Background(),config.LangfuseConfig{Enabled:true,Host:"http://127.0.0.1:1",PublicKey:"pk-test",SecretKey:"sk-test",UserHashSalt:"salt",SampleRate:1})
 if err!=nil{t.Fatal(err)}
 ctx,end=o.Start(context.Background(),"Chat",7,"c","")
 started:=time.Now();obs.Event(ctx,"guard.validation","blocked",map[string]string{"guard_success":"true"});end("")
 if time.Since(started)>time.Second{t.Fatal("request blocked on unavailable exporter")}
 flush,cancel:=context.WithTimeout(context.Background(),100*time.Millisecond);defer cancel();_ = o.Shutdown(flush)
}
