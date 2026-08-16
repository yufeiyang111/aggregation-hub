# Aggregation Hub 桌面端模块边界与组件复用设计

**日期：** 2026-08-16  
**状态：** 已获方案确认，待用户审阅文档后实施  
**关联范围：** Phase 5 桌面端诊断页，以及后续 Dashboard、Requests、Usage 页面  

## 1. 问题与目标

当前 `apps/desktop/src/app/App.tsx` 约 1,284 行，同时承担导航、页面渲染、跨页状态、Provider/Model/Client Config 业务状态、表单、弹窗和 Tauri 调用编排。继续向该文件增加诊断、请求和用量功能会降低局部性、扩大回归范围并使测试难以定位。

目标是在不改变已实现行为、不引入新 UI 依赖、不将秘密放入前端状态的前提下，把桌面端调整为按业务域组织的深模块：调用方只理解小而稳定的接口，复杂状态、表单和调用细节留在模块内部。

## 2. 目标目录与模块

```text
apps/desktop/src/
├─ app/
│  ├─ App.tsx                 # 应用编排、导航、跨页运行时状态
│  └─ AppShell.tsx            # 顶栏、状态菜单、页面容器
├─ pages/
│  ├─ ServicesPage.tsx
│  ├─ ModelsPage.tsx
│  ├─ ClientConfigPage.tsx
│  ├─ DiagnosticsPage.tsx
│  └─ SettingsPage.tsx
├─ features/
│  ├─ providers/              # Provider 状态、操作和对话框
│  ├─ models/                 # 模型列表、筛选、操作和对话框
│  ├─ diagnostics/            # 诊断摘要、导出状态与受控命令
│  └─ connections/            # 既有客户端配置能力
├─ components/
│  ├─ Button.tsx
│  ├─ Dialog.tsx
│  ├─ EmptyState.tsx
│  ├─ StatusDot.tsx
│  └─ PageHeader.tsx
└─ lib/
   └─ desktop-api.ts          # 唯一的 Tauri invoke/DTO 边界
```

## 3. 模块接口与责任

- `App`：只保存页面选择、运行时摘要与跨页协调状态；不直接实现 Provider/Model/Diagnostics 表单或业务流程。目标不超过约 300 行。
- `pages/*`：页面级组合与可访问结构；不直接调用 `invoke`。
- `features/*`：一个业务域的状态、异步保护、错误映射、DTO 转换和对话框组合；页面只消费其稳定 props/hook 接口。
- `components/*`：只收纳至少两个调用点共享、行为一致且可独立测试的展示/交互模块。禁止为了拆文件而制造一次性“通用组件”。
- `lib/desktop-api.ts`：唯一能调用 Tauri `invoke` 的 TypeScript 模块；不保存管理令牌、绝对路径或完整凭据。

## 4. 迁移顺序

1. 先新增通用 `EmptyState`、`StatusDot`、页面壳等已有行为的可复用模块，并让 `App` 使用它们。
2. 以完整业务域为单位迁移 Services，再迁移 Models、Client Config、Settings；每个迁移切片保持原有测试通过。
3. 诊断功能从一开始放入 `features/diagnostics` 和 `pages/DiagnosticsPage.tsx`，不增加 `App` 内部诊断业务逻辑。
4. Dashboard、Requests、Usage 按相同模式新增，禁止回填大段逻辑到 `App`。

## 5. 测试与验收

- 拆分前后保持 `pnpm --dir apps/desktop typecheck`、`lint`、`test --run` 通过。
- 每个页面至少覆盖加载、空、失败、成功四态中与当前能力相关的状态。
- Dialog、导航和状态菜单保持键盘可达、焦点行为不退化。
- 所有 Tauri 调用仍集中在 `desktop-api.ts`，页面代码不出现 `invoke(`。
- 不重写 CSS 视觉系统；复用现有浅色黑白工具风、Hover、Focus 与 Reduced Motion 规则。

## 6. 非目标与风险

- 本设计不引入 Redux、React Query、组件库或新的全局状态依赖。
- 本设计不在重构过程中改变 Data Plane、Core 控制面、凭据存储或 Provider 行为。
- 不要求一次性重写 1,284 行 `App.tsx`；采用小切片迁移，避免重构与新功能失控耦合。
- 在迁移完成前，继续向 `App.tsx` 添加业务逻辑视为架构回退，必须先说明原因并补充测试。
