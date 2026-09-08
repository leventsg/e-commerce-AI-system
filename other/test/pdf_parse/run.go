package pdf_parse

import (
	"context"
	"fmt"
	"os"

	"github.com/cloudwego/eino-ext/components/document/parser/html"
	"github.com/cloudwego/eino-ext/components/document/parser/pdf"
	"github.com/cloudwego/eino/components/document/parser"
)

func main() {
	ctx := context.Background()

	textParser := parser.TextParser{}

	selector := "body"
	htmlParser, _ := html.NewParser(ctx, &html.Config{
		Selector: &selector,
	})

	pdfParser, _ := pdf.NewPDFParser(ctx, &pdf.Config{})

	// 创建扩展解析器
	extParser, _ := parser.NewExtParser(ctx, &parser.ExtParserConfig{
		// 注册特定扩展名的解析器
		Parsers: map[string]parser.Parser{
			".html": htmlParser,
			".pdf":  pdfParser,
		},
		// 设置默认解析器，用于处理未知格式
		FallbackParser: textParser,
	})

	// 使用解析器
	pdfFilePath := "../resume/levent.pdf"
	pdfFile, _ := os.Open(pdfFilePath)

	// htmlFilePath := "../resume/levent-asu-sufied-resume.html"
	// htmlFile, _ := os.Open(htmlFilePath)
	// htmlDocs, _ := extParser.Parse(ctx, htmlFile,
	// 	// 必须提供 URI ExtParser 选择正确的解析器进行解析
	// 	parser.WithURI(htmlFilePath),
	// 	parser.WithExtraMeta(map[string]any{
	// 		"source": "local",
	// 	}),
	// )

	pdfDocs, err := extParser.Parse(ctx, pdfFile,
		// 必须提供 URI ExtParser 选择正确的解析器进行解析
		parser.WithURI(pdfFilePath),
	)
	if err != nil {
		fmt.Printf("pdf文件解析错误: %v\n", err)
		return
	}

	// fmt.Println("html文件解析结果：")
	// for idx, doc := range htmlDocs {
	// 	fmt.Printf("doc_%v content: %v\n", idx, doc.Content)
	// }

	fmt.Println("----------------------------------------")

	fmt.Println("pdf文件解析结果：")
	fmt.Printf("length: %v\n", len(pdfDocs))
	fmt.Printf("docs: %v\n", pdfDocs)
	for idx, doc := range pdfDocs {
		fmt.Printf("doc_%v content: %v\n", idx, doc.Content)
	}
}
