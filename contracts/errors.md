# 错误 JSON 合同

普通业务错误：

```json
{"detail":"..."}
```

HTTP 状态码来自 `BusinessError.status_code`（校验 400、冲突 409、未找到 404、忙碌、队列不可用等）。

结构化校验另带：

```json
{
  "detail": "...",
  "error": {
    "code": "...",
    "message": "...",
    "details": {"issues": [...]}
  }
}
```

未认证走 FastAPI/HTTPException，例如登录失败 `401` + `{"detail":"管理员密钥不正确"}`。

公开错误不得暴露 secret、provider body、文件系统路径或 traceback。
