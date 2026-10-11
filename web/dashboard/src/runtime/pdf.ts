/** PDF text extraction with pdf.js. Output is Markdown with one section per
 *  page so chunk headers cite a page. Layout, tables and OCR are out of scope
 *  (the server runtime's Docling sidecar handles those). */
import * as pdfjs from "pdfjs-dist";
import workerURL from "pdfjs-dist/build/pdf.worker.min.mjs?url";

pdfjs.GlobalWorkerOptions.workerSrc = workerURL;

export async function pdfToMarkdown(name: string, data: Uint8Array): Promise<string> {
  const task = pdfjs.getDocument({ data, useSystemFonts: true });
  const doc = await task.promise;
  try {
    const sections: string[] = [`# ${name.replace(/\.pdf$/i, "")}`];
    for (let page = 1; page <= doc.numPages; page++) {
      const content = await (await doc.getPage(page)).getTextContent();
      let text = "";
      for (const item of content.items) {
        if (!("str" in item)) continue;
        text += item.str + (item.hasEOL ? "\n" : " ");
      }
      text = text.replace(/[ \t]+\n/g, "\n").replace(/\n{3,}/g, "\n\n").trim();
      if (text) sections.push(`## Page ${page}\n\n${text}`);
    }
    if (sections.length === 1) throw new Error("no extractable text (scanned PDFs need OCR)");
    return sections.join("\n\n");
  } finally {
    await task.destroy();
  }
}
