import ReactMarkdown from 'react-markdown'

export function MarkdownRenderer({ content }: { content: string }) {
  return (
    <div className="prose prose-sm prose-invert max-w-none break-words
      prose-headings:text-gray-100 prose-p:text-gray-300 prose-strong:text-gray-200
      prose-code:text-orange-400 prose-code:bg-gray-800 prose-code:px-1 prose-code:py-0.5 prose-code:rounded
      prose-pre:bg-gray-800 prose-pre:border prose-pre:border-gray-700
      prose-a:text-orange-400 prose-li:text-gray-300">
      <ReactMarkdown>{content}</ReactMarkdown>
    </div>
  )
}
