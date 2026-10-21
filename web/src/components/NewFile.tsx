export function NewFileButtons({ onPick, busy }: { onPick: (type: 'docx' | 'xlsx' | 'pptx') => void; busy?: boolean }) {
  return (
    <div className="new-grid">
      <button className="new-tile word" disabled={busy} onClick={() => onPick('docx')}><span className="kind-badge word lg">W</span><b>Document</b><small>.docx</small></button>
      <button className="new-tile cell" disabled={busy} onClick={() => onPick('xlsx')}><span className="kind-badge cell lg">X</span><b>Spreadsheet</b><small>.xlsx</small></button>
      <button className="new-tile slide" disabled={busy} onClick={() => onPick('pptx')}><span className="kind-badge slide lg">P</span><b>Presentation</b><small>.pptx</small></button>
    </div>
  )
}
