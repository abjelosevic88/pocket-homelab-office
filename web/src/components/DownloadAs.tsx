import { Modal } from './ui'
import { qs, V1 } from '../lib/api'

const LABELS: Record<string, string> = {
  pdf: 'PDF', pdfa: 'PDF/A (archival)', docx: 'Word document', docm: 'Word, macro-enabled', dotx: 'Word template', dotm: 'Word template, macro-enabled',
  odt: 'OpenDocument text', ott: 'OpenDocument text template', rtf: 'Rich Text', txt: 'Plain text', html: 'HTML', epub: 'EPUB e-book', fb2: 'FictionBook', md: 'Markdown',
  xlsx: 'Excel workbook', xlsm: 'Excel, macro-enabled', xltx: 'Excel template', xltm: 'Excel template, macro-enabled', ods: 'OpenDocument spreadsheet', ots: 'OpenDocument spreadsheet template', csv: 'CSV', tsv: 'TSV',
  pptx: 'PowerPoint presentation', pptm: 'PowerPoint, macro-enabled', potx: 'PowerPoint template', potm: 'PowerPoint template, macro-enabled', ppsx: 'PowerPoint slideshow', ppsm: 'PowerPoint slideshow, macro-enabled', odp: 'OpenDocument presentation', otp: 'OpenDocument presentation template',
  png: 'PNG image (a zip when several pages)', jpg: 'JPEG image (a zip when several pages)', bmp: 'BMP image', gif: 'GIF image',
}
const IMAGES = new Set(['png', 'jpg', 'bmp', 'gif'])

/** Every type Document Server's conversion service offers for this file. */
export function DownloadAs({ sourceId, path, name, exports, onClose }: { sourceId: number | string; path: string; name: string; exports: string[]; onClose: () => void }) {
  const docs = exports.filter(e => !IMAGES.has(e))
  const imgs = exports.filter(e => IMAGES.has(e))
  const link = (to: string) => `${V1}/sources/${sourceId}/export${qs({ path, to })}`
  return (
    <Modal title={`Download “${name}” as…`} onClose={onClose} size="lg">
      <div className="export-grid">
        {docs.map(e => <a key={e} className="export-tile" href={link(e)} onClick={onClose}><b>.{e}</b><small>{LABELS[e] || e}</small></a>)}
      </div>
      {imgs.length > 0 && <>
        <h4 className="mt">Images</h4>
        <div className="export-grid">
          {imgs.map(e => <a key={e} className="export-tile" href={link(e)} onClick={onClose}><b>.{e}</b><small>{LABELS[e] || e}</small></a>)}
        </div>
      </>}
      <p className="muted small mt">Converted by Document Server; macros survive only in macro-enabled types, and layout-poor types (txt, csv, md) keep text only.</p>
    </Modal>
  )
}
