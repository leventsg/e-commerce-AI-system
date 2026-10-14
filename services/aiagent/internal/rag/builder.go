package rag

import "context"

// RAGBuilder 是知识库检索模块的抽象：负责 RAG 检索，一次 turn 检索一次，结果作为
// 易变输入注入上下文（由 ContextBuilder 组装到易变后缀）。*Service 是其实现。
//
// 检索触发点在 turn 入口（chatlogic），因为 RAG 依赖用户 query 且结果还需用于前端
// 来源展示；检索结果经会话元信息传递给 ContextBuilder，而非在每次模型调用前重检。
type RAGBuilder interface {
	Prepare(ctx context.Context, req PrepareRequest) (*PrepareResult, error)
}

// 编译期断言：*Service 实现 RAGBuilder。
var _ RAGBuilder = (*Service)(nil)
