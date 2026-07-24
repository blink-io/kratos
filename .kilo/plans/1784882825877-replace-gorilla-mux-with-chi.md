# 将 gorilla/mux 替换为 go-chi/chi/v5（100% 行为兼容）

## 目标

将 `transport/http` 中唯一的 mux 依赖替换为 `github.com/go-chi/chi/v5`（要求 v5.2.0+，取最新 v5.3.x），对外 API 与路由行为保持兼容。mux 仅在 `transport/http/{server,context,codec}.go` 中被直接引用；`contrib/{opensergo,otel,polaris}` 通过 `replace ../..` 间接依赖；`contrib/errortracker/sentry` 锁定的是已发布版本，无需改动。

## 已通过 chi 源码（v5.2.1 tree.go / pkg.go.dev v5.3.1）确认的事实

1. chi 正则参数 `{name:regex}` **只在单段内匹配，regex 永远不能匹配 `/`**（`findRoute` 中按 tail=`/` 切段后 `rex.MatchString(xsearch[:p])`）。
2. `*` 通配必须是 pattern 最后一个字符；pattern `/test/prefix*`（`*` 前无 `/`）合法，且匹配 `/test/prefix`、`/test/prefixfoo`、`/test/prefix/123` —— 与 mux `PathPrefix` 完全等价。
3. `Mux.Find(rctx, method, path) string`、`Mux.Match(rctx, method, path) bool`（v5.2.0+）可在不执行 handler 的情况下查询匹配结果（每次须用 `chi.NewRouteContext()`，会污染状态）。
4. `NotFound`/`MethodNotAllowed` 是方法（参数 `http.HandlerFunc`），Mux 无公开字段 → 两个直接读 `srv.router.NotFoundHandler` 的测试必须改写为行为断言。
5. chi 无 Header 匹配路由、无 StrictSlash；`chi.Walk` 会把 `Handle`（全方法）注册的路由按 9 个方法逐个上报，与 mux 语义不同 → 不用于 WalkRoute。
6. 参数名可含 `.`（`{message.name:...}` 合法）。`RoutePattern()` 只有在 endpoint 内才可靠。
7. mux 允许相同 method+path 重复注册（先注册者生效）；chi 静默覆盖（后注册者生效）——接受此差异。
8. mux 按注册顺序匹配；chi 按 静态>正则>参数>通配 优先级——**已确认接受此差异**（用户拍板）。

## 已确认的兼容策略（用户已拍板）

- 路由分发用 chi 原生 trie；通过「模式翻译 + 变量值重组 + 全正则兜底分发器」实现模式层兼容。
- 中间段 `.*` 等 chi 无法翻译的模式：实现基于完整正则的兜底分发器，不 panic。

## 设计

### 1. 新文件 `transport/http/route.go`（兼容层核心）

内部结构，均不导出：

```go
type routeEntry struct {
    method   string   // "" 表示 "*"（全方法）
    pattern  string   // 注册时的原始 mux 风格 pattern（WalkRoute/pathTemplate 用）
    chiPat   string   // 翻译后的 chi pattern
    fullRE   *regexp.Regexp // 兜底匹配用（仅 fallback 条目）
    kind     routeKind // normal | prefix | fallback | headerDispatch
}
```

**模式翻译** `translatePattern(pattern string) (chiPat string, recon []varRecon, fallback *regexp.Regexp)`：

- 用正则 `\{([^{}:]+)(?::([^{}]*))?\}` 扫描每个变量：
  - 无 regex（`{name}`）→ 原样保留。
  - regex 不含 `/` 且不含 `.*`（如 `[0-9]+`、`[^/]+`）→ 原样保留（chi 单段语义与 mux 一致）。
  - regex 含 `/`（来自生成器 `{name=publishers/*/books/*}` → `publishers/[^/]+/books/[^/]+`，或手写）→ 按 `/` 切分：
    - 所有子段均为 字面量 或 单段正则 → chi pattern 中把字面量内联、每个非字面量子段替换为合成参数 `{name__0}`、`{name__1}`…；记录 `varRecon{name, template}`，template 形如 `["publishers", param0, "books", param1]`，请求时把 chi URLParams 按 template 用 `/` 拼接回填为 `name` 的值，并删除合成键。
    - 子段为 `.*` 且是变量的最后一段、且该变量位于整个 pattern 末尾 → 翻译为 chi 结尾 `*`；recon 时 `value = strings.Join(前缀字面量+`/`+URLParam("*"))`。
    - 其他情况（中间段 `.*`）→ 不注册 chi 原生路由，编译 mux 风格**全路径命名正则** `^...(?P<name>...)...$`，走兜底分发器（见 §4）。
- `{name}` 的键名含 `.` 合法；合成键 `{name__N}` 需保证 pattern 内唯一。

**路由注册表** `routeRegistry`（Server 持有，`sync.RWMutex` 保护，因 PathPrefix/注册可在 NewServer 之后发生）：

- 按注册顺序保存 `routeEntry`（保序，模拟 mux Walk 顺序）。
- `byChiPat map[string]string`：chi pattern → 原始 pattern（filter 中经 `Mux.Find` 反查 pathTemplate）。

**变量重组 wrapper**：对 recon 非空的条目，注册时用 wrapper 包装 handler：在 endpoint 内（此时 chi 已完成匹配）取 `chi.RouteContext(r.Context()).URLParams`，按 recon 重写 Keys/Values 后调用原 handler。`Vars()`/`DefaultRequestVars` 只需原样读 URLParams。

### 2. `server.go` 改造

- `router *mux.Router` → 两个字段：`root *chi.Mux`（ServeHTTP/Find/兜底分发用，永不变）+ `router chi.Router`（注册目标，`PathPrefix` option 会替换为 sub-router）。
- `NewServer`：`root = chi.NewRouter()`；NotFound/MethodNotAllowed 用 `root.NotFound(http.DefaultServeMux.ServeHTTP)`、`root.MethodNotAllowed(...)`；`root.Use(srv.filter())`（`filter()` 返回类型改为 `func(http.Handler) http.Handler`）；`StrictSlash` 不再调用 chi（无此 API），仅存 `srv.strictSlash`，在 filter 中实现（见 §5）。
- `PathPrefix(prefix)` option：`sub := chi.NewRouter(); s.router.Mount(prefix, sub); s.router = sub`（chi Mount 支持挂载后继续注册）。
- `Handle(path, h)`：`root/router.Handle(translatedPath, wrap(h))` + 登记 registry（method=""）。
- `HandleFunc`：同上（HandlerFunc 包装）。
- `HandlePrefix(prefix, h)`：注册 chi pattern `prefix + "*"`（已验证与 mux PathPrefix 等价）+ 登记 registry（kind=prefix）。
- `HandleHeader(key, val, h)`：见 §4。
- `Router.Handle`（router.go）：`path.Join` 不变；method != "*" 时 `router.Method(method, chiPat, wrapped)`（非标准方法先 `chi.RegisterMethod(method)`）；method == "*" 时 `router.Handle(chiPat, wrapped)`；登记 registry。
- `WalkRoute`：改为遍历 registry（注册顺序）；跳过 method=="*" 的条目（mux `GetMethods` 报错被忽略的旧行为）；kind=prefix 条目返回与 mux 等价的 "route doesn't have a path template" 错误（保持现行行为）；header 条目跳过（无 method）。
- `filter()`：
  - pathTemplate 解析：用 `srv.root.Find(chi.NewRouteContext(), req.Method, req.URL.Path)` 得 chi pattern → `registry.byChiPat` 反查原始 pattern；查不到（404/405/分发器）→ 用 `req.URL.Path`（与 mux `CurrentRoute==nil` 分支一致）。兜底/header 分发器在自己选中条目后，从 context 取 `*Transport` 直接设置 `operation`/`pathTemplate` 为该条目原始 pattern。
  - StrictSlash 逻辑放在 filter 前置（见 §5）。
  - 其余（timeout、Transport 注入）不变。

### 3. `context.go` / `codec.go` 改造

- 删除 `github.com/gorilla/mux` import，新增内部函数 `routeVars(r *http.Request) map[string]string`：从 `chi.RouteContext(r.Context())` 读 `URLParams`（**必须 nil 检查**，mux.Vars 对无路由上下文的请求返回空 map）；此时 URLParams 已被 recon wrapper 重写成 mux 语义。
- `wrapper.Vars()` 与 `DefaultRequestVars` 改用 `routeVars`。

### 4. Header 路由与正则兜底的分发器

- `HandleHeader`：懒注册一次 chi `/*`（`root.Handle("/*", dispatcher)`）；dispatcher 按注册顺序遍历 header 条目，`req.Header.Get(key)==val` 命中则执行。全部未命中时：遍历 9 个标准方法用 `root.Match` 判断是否为 405（path 命中但 method 不匹配，模拟 mux 行为），是则调 `MethodNotAllowedHandler`，否则 `NotFoundHandler`。
- 正则兜底（中间段 `.*` 等）：按最长公共静态前缀 `P` 聚合，注册 chi `P + "*"` 到 fallback 分发器；分发器按注册顺序用各条目 `fullRE` 全路径匹配（命名组提取 vars 写入 URLParams），首个命中者执行并设置 pathTemplate；未命中回落 NotFound/405（同 header 分发器逻辑）。
- 两类分发器条目均登记 registry（header 条目标记 kind=headerDispatch，StrictSlash/Find 反查时忽略）。

### 5. StrictSlash（mux 双向语义，默认开启）

在 `filter()` 最前面（仅 `srv.strictSlash==true`）：

1. `p := req.URL.Path`；若 `p == "/"` 直接放行。
2. 用 `root.Match(chi.NewRouteContext(), req.Method, p)` 判断直接命中；若命中结果只是 header 分发器的 `/*`（用 Find + registry 判定），视为未命中。
3. 未命中时构造 toggled：以 `/` 结尾则去掉，否则加 `/`；`root.Match(rctx2, req.Method, toggled)` 命中真实路由 → `http.Redirect(w, req, toggled(+原 query), http.StatusMovedPermanently)`（mux 恒用 301）。
4. 否则放行（走 404/405）。

### 6. 测试调整（仅内部断言，不改行为断言）

- `server_test.go` `TestNotFoundHandler`/`TestMethodNotAllowedHandler`：改为行为断言（自定义 handler 返回特定状态码，请求未知路径/错误方法验证被调用）。
- 其余测试（TestServer 含 header/prefix/wildcard/regex 路由、TestRoute、TestRouterHandleWildcardMethod、WalkRoute、binding、codec、context 测试）均应原样通过。
- 新增单测（`route_test.go` 或加入现有测试文件）：模式翻译（`{id:[0-9]+}`、`{name=publishers/*/books/*}` 重组值、`{name=shelves/**}` 结尾通配、中间 `.*` 兜底）、StrictSlash 双向 301、HandlePrefix 的 `/prefixfoo` 边界、405 语义。

### 7. 依赖清理

- 根模块：`go get github.com/go-chi/chi/v5@latest && go mod tidy`（移除 gorilla/mux，保留 gorilla/websocket）。
- `contrib/{opensergo,otel,polaris}`：各自 `go mod tidy`（mux indirect 消失，chi 进入 indirect）。
- `contrib/errortracker/sentry`：无 replace，锁定已发布 kratos，不改动。

## 接受的残余差异（计划中明确记录，不视为 bug）

1. 重叠模式（如 `/users/new` 与 `/users/{name}` 同时注册）时匹配优先级不同：chi 静态优先，mux 注册顺序优先。
2. 相同 method+path 重复注册：chi 后者覆盖，mux 前者生效。
3. `/v1/{name=shelves/**}` 类结尾 `.*` 翻译成 `*` 后，会额外匹配不带尾 `/` 的裸前缀路径（mux 要求字面 `/`）。
4. `PathPrefix` option 挂载的 sub-router 中注册的路由，`Mux.Find` 返回的 pattern 可能不含挂载前缀，pathTemplate 反查不到时退化为原始请求路径（mux 行为也是退化，只是值略不同）。
5. 仅注册过 HandleHeader 时，header 路由在 mux 中会按注册顺序参与匹配、可能遮蔽后注册路由；chi 实现中 header 分发器永远最后兜底。

## 实施顺序

1. `go get github.com/go-chi/chi/v5@latest`
2. 新增 `transport/http/route.go`（翻译/重组/registry/分发器/StrictSlash helpers）
3. 改 `server.go`、`router.go`、`context.go`、`codec.go`
4. 改 `server_test.go` 两个内部断言测试 + 新增兼容层单测
5. `go build ./... && go test ./transport/http/... && go test ./cmd/protoc-gen-go-http/... && go vet ./...`
6. 全量 `go test ./...`
7. contrib 三模块 `go mod tidy` 并 `go build ./...` 验证

## 验证

- 根模块 `go test ./...` 全绿（现有测试即兼容契约，特别是 TestServer 的 `/index/{id:[0-9]+}`、`/test/prefix/123111`、header 路由、405/404 语义）。
- `gofmt -l .` 无输出、`go vet ./...` 通过。
- contrib/{opensergo,otel,polaris} 各自 `go build ./...` 通过。
- 全局 `grep -r "gorilla/mux" --include="*.go" .` 无残留（go.sum 除外，sentry 模块除外）。
