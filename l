=== RUN   TestWorkload
2023-01-24T20:35:54.323311Z	error	cv2	howardjohn: Computing mesh config
2023-01-24T20:35:54.323327Z	error	cv2	howardjohn: *cv2.handler[istio.io/api/mesh/v1alpha1.MeshConfig] not a registerer
2023-01-24T20:35:54.323335Z	error	cv2	howardjohn: Fetch: register ConfigMap/istio-user: {name:istio-user namespace: selects:map[]}
2023-01-24T20:35:54.323337Z	error	cv2	howardjohn: *cv2.handler[istio.io/api/mesh/v1alpha1.MeshConfig] not a registerer
2023-01-24T20:35:54.323339Z	error	cv2	howardjohn: Fetch: register ConfigMap/istio: {name:istio namespace: selects:map[]}
2023-01-24T20:35:54.323340Z	error	cv2	howardjohn: computed mesh config to istio
2023-01-24T20:35:54.323344Z	error	cv2	howardjohn: updated true
2023-01-24T20:35:54.323348Z	info	cv2	insert dep, filter: {name:istio-user namespace: selects:map[]}	dep=ConfigMap/istio-user
2023-01-24T20:35:54.323356Z	info	cv2	insert dep, filter: {name:istio namespace: selects:map[]}	dep=ConfigMap/istio
    example_test.go:74: insert user
2023-01-24T20:35:54.323415Z	debug	cv2	got event add	dep=ConfigMap/istio-user
2023-01-24T20:35:54.323417Z	debug	cv2	matches name: "istio-user" vs "istio-user"
2023-01-24T20:35:54.323419Z	debug	cv2	matches namespace: "" vs "istio-system"
2023-01-24T20:35:54.323423Z	debug	cv2	obj *v1.ConfigMap has no Spec
2023-01-24T20:35:54.323425Z	debug	cv2	matches selects: map[] vs map[]
2023-01-24T20:35:54.323427Z	debug	cv2	Add match istio-user	dep=ConfigMap/istio-user
2023-01-24T20:35:54.323429Z	error	cv2	howardjohn: Computing mesh config
2023-01-24T20:35:54.323433Z	error	cv2	howardjohn: *cv2.handler[istio.io/api/mesh/v1alpha1.MeshConfig] not a registerer
2023-01-24T20:35:54.323435Z	error	cv2	howardjohn: Fetch: register ConfigMap/istio-user: {name:istio-user namespace: selects:map[]}
2023-01-24T20:35:54.323438Z	debug	cv2	matches name: "istio-user" vs "istio-user"
2023-01-24T20:35:54.323439Z	debug	cv2	matches namespace: "" vs "istio-system"
2023-01-24T20:35:54.323442Z	debug	cv2	obj *v1.ConfigMap has no Spec
2023-01-24T20:35:54.323443Z	debug	cv2	matches selects: map[] vs map[]
2023-01-24T20:35:54.323445Z	error	cv2	howardjohn: *cv2.handler[istio.io/api/mesh/v1alpha1.MeshConfig] not a registerer
2023-01-24T20:35:54.323447Z	error	cv2	howardjohn: Fetch: register ConfigMap/istio: {name:istio namespace: selects:map[]}
2023-01-24T20:35:54.323449Z	debug	cv2	no match name: "istio" vs "istio-user"
2023-01-24T20:35:54.324115Z	error	cv2	howardjohn: computed mesh config to user
2023-01-24T20:35:54.324137Z	error	cv2	howardjohn: updated true
2023-01-24T20:35:54.324153Z	debug	cv2	got event add	dep=ConfigMap/istio
2023-01-24T20:35:54.324155Z	debug	cv2	no match name: "istio" vs "istio-user"
2023-01-24T20:35:54.324157Z	debug	cv2	Add no match istio-user	dep=ConfigMap/istio
    example_test.go:79: user
    example_test.go:82: create core
2023-01-24T20:35:54.373868Z	debug	cv2	got event add	dep=ConfigMap/istio
2023-01-24T20:35:54.373896Z	debug	cv2	matches name: "istio" vs "istio"
2023-01-24T20:35:54.373906Z	debug	cv2	matches namespace: "" vs "istio-system"
2023-01-24T20:35:54.373924Z	debug	cv2	obj *v1.ConfigMap has no Spec
2023-01-24T20:35:54.373934Z	debug	cv2	matches selects: map[] vs map[]
2023-01-24T20:35:54.373941Z	debug	cv2	Add match istio	dep=ConfigMap/istio
2023-01-24T20:35:54.373948Z	error	cv2	howardjohn: Computing mesh config
2023-01-24T20:35:54.373973Z	error	cv2	howardjohn: *cv2.handler[istio.io/api/mesh/v1alpha1.MeshConfig] not a registerer
2023-01-24T20:35:54.373987Z	error	cv2	howardjohn: Fetch: register ConfigMap/istio-user: {name:istio-user namespace: selects:map[]}
2023-01-24T20:35:54.373998Z	debug	cv2	matches name: "istio-user" vs "istio-user"
2023-01-24T20:35:54.374004Z	debug	cv2	matches namespace: "" vs "istio-system"
2023-01-24T20:35:54.374012Z	debug	cv2	obj *v1.ConfigMap has no Spec
2023-01-24T20:35:54.374018Z	debug	cv2	matches selects: map[] vs map[]
2023-01-24T20:35:54.374024Z	debug	cv2	no match name: "istio-user" vs "istio"
2023-01-24T20:35:54.374031Z	error	cv2	howardjohn: *cv2.handler[istio.io/api/mesh/v1alpha1.MeshConfig] not a registerer
2023-01-24T20:35:54.374052Z	error	cv2	howardjohn: Fetch: register ConfigMap/istio: {name:istio namespace: selects:map[]}
2023-01-24T20:35:54.374065Z	debug	cv2	no match name: "istio" vs "istio-user"
2023-01-24T20:35:54.374071Z	debug	cv2	matches name: "istio" vs "istio"
2023-01-24T20:35:54.374079Z	debug	cv2	matches namespace: "" vs "istio-system"
2023-01-24T20:35:54.374092Z	debug	cv2	obj *v1.ConfigMap has no Spec
2023-01-24T20:35:54.374098Z	debug	cv2	matches selects: map[] vs map[]
2023-01-24T20:35:54.374453Z	error	cv2	howardjohn: computed mesh config to core
2023-01-24T20:35:54.374482Z	error	cv2	howardjohn: updated true
2023-01-24T20:35:54.374499Z	debug	cv2	got event add	dep=ConfigMap/istio-user
2023-01-24T20:35:54.374505Z	debug	cv2	no match name: "istio-user" vs "istio"
2023-01-24T20:35:54.374512Z	debug	cv2	Add no match istio	dep=ConfigMap/istio-user
    example_test.go:89: update core to alt
2023-01-24T20:35:54.435186Z	debug	cv2	got event update	dep=ConfigMap/istio
2023-01-24T20:35:54.435222Z	debug	cv2	matches name: "istio" vs "istio"
2023-01-24T20:35:54.435233Z	debug	cv2	matches namespace: "" vs "istio-system"
2023-01-24T20:35:54.435265Z	debug	cv2	obj *v1.ConfigMap has no Spec
2023-01-24T20:35:54.435293Z	debug	cv2	matches selects: map[] vs map[]
2023-01-24T20:35:54.435308Z	debug	cv2	Update match istio	dep=ConfigMap/istio
2023-01-24T20:35:54.435318Z	error	cv2	howardjohn: Computing mesh config
2023-01-24T20:35:54.435354Z	error	cv2	howardjohn: *cv2.handler[istio.io/api/mesh/v1alpha1.MeshConfig] not a registerer
2023-01-24T20:35:54.435374Z	error	cv2	howardjohn: Fetch: register ConfigMap/istio-user: {name:istio-user namespace: selects:map[]}
2023-01-24T20:35:54.435393Z	debug	cv2	no match name: "istio-user" vs "istio"
2023-01-24T20:35:54.435405Z	debug	cv2	matches name: "istio-user" vs "istio-user"
2023-01-24T20:35:54.435414Z	debug	cv2	matches namespace: "" vs "istio-system"
2023-01-24T20:35:54.435428Z	debug	cv2	obj *v1.ConfigMap has no Spec
2023-01-24T20:35:54.435440Z	debug	cv2	matches selects: map[] vs map[]
2023-01-24T20:35:54.435451Z	error	cv2	howardjohn: *cv2.handler[istio.io/api/mesh/v1alpha1.MeshConfig] not a registerer
2023-01-24T20:35:54.435465Z	error	cv2	howardjohn: Fetch: register ConfigMap/istio: {name:istio namespace: selects:map[]}
2023-01-24T20:35:54.435480Z	debug	cv2	no match name: "istio" vs "istio-user"
2023-01-24T20:35:54.435490Z	debug	cv2	matches name: "istio" vs "istio"
2023-01-24T20:35:54.435498Z	debug	cv2	matches namespace: "" vs "istio-system"
2023-01-24T20:35:54.435510Z	debug	cv2	obj *v1.ConfigMap has no Spec
2023-01-24T20:35:54.435520Z	debug	cv2	matches selects: map[] vs map[]
2023-01-24T20:35:54.435980Z	error	cv2	howardjohn: computed mesh config to alt
2023-01-24T20:35:54.436019Z	error	cv2	howardjohn: updated true
2023-01-24T20:35:54.436039Z	debug	cv2	got event update	dep=ConfigMap/istio-user
2023-01-24T20:35:54.436046Z	debug	cv2	no match name: "istio-user" vs "istio"
2023-01-24T20:35:54.436052Z	debug	cv2	no match name: "istio-user" vs "istio"
2023-01-24T20:35:54.436058Z	debug	cv2	Update no change	dep=ConfigMap/istio-user
    example_test.go:96: NOP change
2023-01-24T20:35:54.495543Z	debug	cv2	got event update	dep=ConfigMap/istio
2023-01-24T20:35:54.495568Z	debug	cv2	matches name: "istio" vs "istio"
2023-01-24T20:35:54.495576Z	debug	cv2	matches namespace: "" vs "istio-system"
2023-01-24T20:35:54.495592Z	debug	cv2	obj *v1.ConfigMap has no Spec
2023-01-24T20:35:54.495628Z	debug	cv2	matches selects: map[] vs map[]
2023-01-24T20:35:54.495642Z	debug	cv2	Update match istio	dep=ConfigMap/istio
2023-01-24T20:35:54.495650Z	error	cv2	howardjohn: Computing mesh config
2023-01-24T20:35:54.495673Z	error	cv2	howardjohn: *cv2.handler[istio.io/api/mesh/v1alpha1.MeshConfig] not a registerer
2023-01-24T20:35:54.495687Z	error	cv2	howardjohn: Fetch: register ConfigMap/istio-user: {name:istio-user namespace: selects:map[]}
2023-01-24T20:35:54.495699Z	debug	cv2	matches name: "istio-user" vs "istio-user"
2023-01-24T20:35:54.495704Z	debug	cv2	matches namespace: "" vs "istio-system"
2023-01-24T20:35:54.495713Z	debug	cv2	obj *v1.ConfigMap has no Spec
2023-01-24T20:35:54.495730Z	debug	cv2	matches selects: map[] vs map[]
2023-01-24T20:35:54.495737Z	debug	cv2	no match name: "istio-user" vs "istio"
2023-01-24T20:35:54.495744Z	error	cv2	howardjohn: *cv2.handler[istio.io/api/mesh/v1alpha1.MeshConfig] not a registerer
2023-01-24T20:35:54.495752Z	error	cv2	howardjohn: Fetch: register ConfigMap/istio: {name:istio namespace: selects:map[]}
2023-01-24T20:35:54.495759Z	debug	cv2	no match name: "istio" vs "istio-user"
2023-01-24T20:35:54.495765Z	debug	cv2	matches name: "istio" vs "istio"
2023-01-24T20:35:54.495770Z	debug	cv2	matches namespace: "" vs "istio-system"
2023-01-24T20:35:54.495778Z	debug	cv2	obj *v1.ConfigMap has no Spec
2023-01-24T20:35:54.495784Z	debug	cv2	matches selects: map[] vs map[]
2023-01-24T20:35:54.496104Z	error	cv2	howardjohn: computed mesh config to alt
2023-01-24T20:35:54.497333Z	error	cv2	howardjohn: updated false
2023-01-24T20:35:54.497360Z	debug	cv2	got event update	dep=ConfigMap/istio-user
2023-01-24T20:35:54.497368Z	debug	cv2	no match name: "istio-user" vs "istio"
2023-01-24T20:35:54.497375Z	debug	cv2	no match name: "istio-user" vs "istio"
2023-01-24T20:35:54.497381Z	debug	cv2	Update no change	dep=ConfigMap/istio-user
    example_test.go:104: update core back
2023-01-24T20:35:54.546766Z	debug	cv2	got event update	dep=ConfigMap/istio
2023-01-24T20:35:54.546795Z	debug	cv2	matches name: "istio" vs "istio"
2023-01-24T20:35:54.546806Z	debug	cv2	matches namespace: "" vs "istio-system"
2023-01-24T20:35:54.546835Z	debug	cv2	obj *v1.ConfigMap has no Spec
2023-01-24T20:35:54.546861Z	debug	cv2	matches selects: map[] vs map[]
2023-01-24T20:35:54.546875Z	debug	cv2	Update match istio	dep=ConfigMap/istio
2023-01-24T20:35:54.546885Z	error	cv2	howardjohn: Computing mesh config
2023-01-24T20:35:54.546913Z	error	cv2	howardjohn: *cv2.handler[istio.io/api/mesh/v1alpha1.MeshConfig] not a registerer
2023-01-24T20:35:54.546929Z	error	cv2	howardjohn: Fetch: register ConfigMap/istio-user: {name:istio-user namespace: selects:map[]}
2023-01-24T20:35:54.546941Z	debug	cv2	matches name: "istio-user" vs "istio-user"
2023-01-24T20:35:54.546947Z	debug	cv2	matches namespace: "" vs "istio-system"
2023-01-24T20:35:54.546955Z	debug	cv2	obj *v1.ConfigMap has no Spec
2023-01-24T20:35:54.546969Z	debug	cv2	matches selects: map[] vs map[]
2023-01-24T20:35:54.546979Z	debug	cv2	no match name: "istio-user" vs "istio"
2023-01-24T20:35:54.546985Z	error	cv2	howardjohn: *cv2.handler[istio.io/api/mesh/v1alpha1.MeshConfig] not a registerer
2023-01-24T20:35:54.546995Z	error	cv2	howardjohn: Fetch: register ConfigMap/istio: {name:istio namespace: selects:map[]}
2023-01-24T20:35:54.547007Z	debug	cv2	no match name: "istio" vs "istio-user"
2023-01-24T20:35:54.547013Z	debug	cv2	matches name: "istio" vs "istio"
2023-01-24T20:35:54.547019Z	debug	cv2	matches namespace: "" vs "istio-system"
2023-01-24T20:35:54.547027Z	debug	cv2	obj *v1.ConfigMap has no Spec
2023-01-24T20:35:54.547033Z	debug	cv2	matches selects: map[] vs map[]
2023-01-24T20:35:54.547412Z	error	cv2	howardjohn: computed mesh config to core
2023-01-24T20:35:54.547444Z	error	cv2	howardjohn: updated true
2023-01-24T20:35:54.547461Z	debug	cv2	got event update	dep=ConfigMap/istio-user
2023-01-24T20:35:54.547469Z	debug	cv2	no match name: "istio-user" vs "istio"
2023-01-24T20:35:54.547475Z	debug	cv2	no match name: "istio-user" vs "istio"
2023-01-24T20:35:54.547481Z	debug	cv2	Update no change	dep=ConfigMap/istio-user
    example_test.go:111: delete core
2023-01-24T20:35:54.608104Z	debug	cv2	got event delete	dep=ConfigMap/istio
2023-01-24T20:35:54.608130Z	debug	cv2	matches name: "istio" vs "istio"
2023-01-24T20:35:54.608138Z	debug	cv2	matches namespace: "" vs "istio-system"
2023-01-24T20:35:54.608154Z	debug	cv2	obj *v1.ConfigMap has no Spec
2023-01-24T20:35:54.608166Z	debug	cv2	matches selects: map[] vs map[]
2023-01-24T20:35:54.608175Z	debug	cv2	delete match istio	dep=ConfigMap/istio
2023-01-24T20:35:54.608181Z	error	cv2	howardjohn: Computing mesh config
2023-01-24T20:35:54.608211Z	error	cv2	howardjohn: *cv2.handler[istio.io/api/mesh/v1alpha1.MeshConfig] not a registerer
2023-01-24T20:35:54.608226Z	error	cv2	howardjohn: Fetch: register ConfigMap/istio-user: {name:istio-user namespace: selects:map[]}
2023-01-24T20:35:54.608237Z	debug	cv2	matches name: "istio-user" vs "istio-user"
2023-01-24T20:35:54.608243Z	debug	cv2	matches namespace: "" vs "istio-system"
2023-01-24T20:35:54.608252Z	debug	cv2	obj *v1.ConfigMap has no Spec
2023-01-24T20:35:54.608258Z	debug	cv2	matches selects: map[] vs map[]
2023-01-24T20:35:54.608277Z	error	cv2	howardjohn: *cv2.handler[istio.io/api/mesh/v1alpha1.MeshConfig] not a registerer
2023-01-24T20:35:54.608286Z	error	cv2	howardjohn: Fetch: register ConfigMap/istio: {name:istio namespace: selects:map[]}
2023-01-24T20:35:54.608294Z	debug	cv2	no match name: "istio" vs "istio-user"
2023-01-24T20:35:54.608521Z	error	cv2	howardjohn: computed mesh config to user
2023-01-24T20:35:54.608565Z	error	cv2	howardjohn: updated true
2023-01-24T20:35:54.608581Z	debug	cv2	got event delete	dep=ConfigMap/istio-user
2023-01-24T20:35:54.608588Z	debug	cv2	no match name: "istio-user" vs "istio"
2023-01-24T20:35:54.608595Z	debug	cv2	Add no match istio	dep=ConfigMap/istio-user
    example_test.go:118: done
    example_test.go:162: spawn workload collectioner
    example_test.go:205: pod1 create
2023-01-24T20:35:54.671694Z	debug	cv2	got event add	dep=primary
2023-01-24T20:35:54.671758Z	error	cv2	howardjohn: computing workload for pod pod1
2023-01-24T20:35:54.671776Z	error	cv2	howardjohn: registerer
2023-01-24T20:35:54.671787Z	error	cv2	howardjohn: register handler
2023-01-24T20:35:54.671831Z	error	cv2	howardjohn: Fetch: register AuthorizationPolicy/: {name: namespace: selects:map[app:bar]}
2023-01-24T20:35:54.671848Z	error	cv2	howardjohn: registerer
2023-01-24T20:35:54.671857Z	error	cv2	howardjohn: register handler
2023-01-24T20:35:54.671885Z	error	cv2	howardjohn: Fetch: register Service/: {name: namespace: selects:map[app:bar]}
2023-01-24T20:35:54.671935Z	error	cv2	howardjohn: updated true
    example_test.go:213: pod2 create
2023-01-24T20:35:54.722636Z	debug	cv2	got event add	dep=primary
2023-01-24T20:35:54.722667Z	error	cv2	howardjohn: computing workload for pod pod2
2023-01-24T20:35:54.722684Z	error	cv2	howardjohn: registerer
2023-01-24T20:35:54.722719Z	error	cv2	howardjohn: Fetch: register AuthorizationPolicy/: {name: namespace: selects:map[app:bar]}
2023-01-24T20:35:54.722734Z	error	cv2	howardjohn: registerer
2023-01-24T20:35:54.722755Z	error	cv2	howardjohn: Fetch: register Service/: {name: namespace: selects:map[app:bar]}
2023-01-24T20:35:54.722796Z	error	cv2	howardjohn: updated true
    example_test.go:220: pod3 create
    example_test.go:227: svc create
2023-01-24T20:35:54.823619Z	error	cv2	howardjohn: handler2 call for default/svc1, deps=2
2023-01-24T20:35:54.823645Z	error	cv2	howardjohn: handler2 lock
2023-01-24T20:35:54.823670Z	error	cv2	howardjohn: deps AuthorizationPolicy/, compare to Service/svc1 and Service/
2023-01-24T20:35:54.823686Z	error	cv2	howardjohn: deps Service/, compare to Service/svc1 and Service/
2023-01-24T20:35:54.823698Z	debug	cv2	matches name: "" vs "svc1"
2023-01-24T20:35:54.823709Z	debug	cv2	matches namespace: "" vs "default"
2023-01-24T20:35:54.823741Z	debug	cv2	matches selects: map["app":"bar"] vs map["app":"bar"]
2023-01-24T20:35:54.823760Z	info	cv2	event for Service/	match=true
2023-01-24T20:35:54.823777Z	error	cv2	howardjohn: deps AuthorizationPolicy/, compare to Service/svc1 and Service/
2023-01-24T20:35:54.823791Z	error	cv2	howardjohn: deps Service/, compare to Service/svc1 and Service/
2023-01-24T20:35:54.823801Z	debug	cv2	matches name: "" vs "svc1"
2023-01-24T20:35:54.823811Z	debug	cv2	matches namespace: "" vs "default"
2023-01-24T20:35:54.823829Z	debug	cv2	matches selects: map["app":"bar"] vs map["app":"bar"]
2023-01-24T20:35:54.823842Z	info	cv2	event for Service/	match=true
2023-01-24T20:35:54.823852Z	error	cv2	howardjohn: handler2 call matched=2
2023-01-24T20:35:54.823870Z	error	cv2	howardjohn: computing workload for pod pod1
2023-01-24T20:35:54.823885Z	error	cv2	howardjohn: registerer
2023-01-24T20:35:54.823909Z	error	cv2	howardjohn: Fetch: register AuthorizationPolicy/: {name: namespace: selects:map[app:bar]}
2023-01-24T20:35:54.823924Z	error	cv2	howardjohn: registerer
2023-01-24T20:35:54.823939Z	error	cv2	howardjohn: Fetch: register Service/: {name: namespace: selects:map[app:bar]}
2023-01-24T20:35:54.824277Z	error	cv2	howardjohn: updated false
2023-01-24T20:35:54.824295Z	error	cv2	howardjohn: computing workload for pod pod2
2023-01-24T20:35:54.824306Z	error	cv2	howardjohn: registerer
2023-01-24T20:35:54.824325Z	error	cv2	howardjohn: Fetch: register AuthorizationPolicy/: {name: namespace: selects:map[app:bar]}
2023-01-24T20:35:54.824336Z	error	cv2	howardjohn: registerer
2023-01-24T20:35:54.824353Z	error	cv2	howardjohn: Fetch: register Service/: {name: namespace: selects:map[app:bar]}
2023-01-24T20:35:54.824391Z	error	cv2	howardjohn: updated false
    example_test.go:235: Final workload: name:"pod1"  namespace:"default"  address:"\n\x00\x00\x01"  network:"TODO"  canonical_name:"bar"  canonical_revision:"latest"  workload_type:POD  workload_name:"pod1"
    example_test.go:235: Final workload: name:"pod2"  namespace:"default"  address:"\n\x00\x00\x02"  network:"TODO"  canonical_name:"bar"  canonical_revision:"latest"  workload_type:POD  workload_name:"pod2"
--- PASS: TestWorkload (0.55s)
PASS
ok  	istio.io/istio/pkg/kube/cv2	0.654s
