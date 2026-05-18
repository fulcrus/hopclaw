// ---------------------------------------------------------------------------
// Markdown renderer (lightweight, no dependencies)
// ---------------------------------------------------------------------------

import { safeExternalURL, safeImageSource } from './linking.js';

/**
 * Escape HTML special characters.
 * @param {string} s - Raw text.
 * @returns {string} HTML-safe text.
 */
function esc(s) {
  const d = document.createElement('div');
  d.appendChild(document.createTextNode(s));
  return d.innerHTML;
}

/**
 * Render a full markdown string to HTML.
 * Supports: code blocks, headings, blockquotes, lists, hr, tables, paragraphs.
 * @param {string} text - Markdown source.
 * @returns {string} HTML string.
 */
export function renderMarkdown(text) {
  if (!text) return '';

  const lines = text.split('\n');
  let html = '';
  let inCodeBlock = false;
  let codeLang = '';
  let codeLines = [];
  let inList = false;
  let listType = '';
  let tableRows = [];
  let inTable = false;

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];

    // ----- Code blocks -----
    if (line.match(/^```/)) {
      if (inCodeBlock) {
        const langClass = codeLang ? ' class="lang-' + esc(codeLang) + '"' : '';
        html += '<pre><code' + langClass + '>' + esc(codeLines.join('\n')) + '</code>'
             + '<button class="hc-code-copy" onclick="HC.copyCode(this)">Copy</button></pre>';
        inCodeBlock = false;
        codeLines = [];
        codeLang = '';
      } else {
        if (inTable) { html += buildTable(tableRows); tableRows = []; inTable = false; }
        if (inList) { html += closeList(listType); inList = false; }
        inCodeBlock = true;
        codeLang = line.replace(/^```/, '').trim();
      }
      continue;
    }

    if (inCodeBlock) {
      codeLines.push(line);
      continue;
    }

    // ----- Table detection -----
    if (line.match(/^\|(.+)\|$/)) {
      if (inList) { html += closeList(listType); inList = false; }
      // Check if next line is separator (|---|---|)
      if (!inTable) {
        const nextLine = i + 1 < lines.length ? lines[i + 1] : '';
        if (nextLine.match(/^\|[\s\-:|]+\|$/)) {
          inTable = true;
          tableRows.push(line);
          continue;
        }
      }
      if (inTable) {
        // Skip separator row
        if (line.match(/^\|[\s\-:|]+\|$/)) continue;
        tableRows.push(line);
        continue;
      }
    } else if (inTable) {
      html += buildTable(tableRows);
      tableRows = [];
      inTable = false;
      // Fall through to process current line normally
    }

    // ----- Empty line -----
    if (line.trim() === '') {
      if (inList) { html += closeList(listType); inList = false; }
      continue;
    }

    // ----- Headings (h1-h6) -----
    const hm = line.match(/^(#{1,6})\s+(.*)/);
    if (hm) {
      if (inList) { html += closeList(listType); inList = false; }
      const level = hm[1].length;
      html += '<h' + (level + 1) + '>' + inlineMarkdown(hm[2]) + '</h' + (level + 1) + '>';
      continue;
    }

    // ----- Blockquote -----
    if (line.match(/^>\s*/)) {
      if (inList) { html += closeList(listType); inList = false; }
      html += '<blockquote>' + inlineMarkdown(line.replace(/^>\s*/, '')) + '</blockquote>';
      continue;
    }

    // ----- Unordered list -----
    const ulm = line.match(/^(\s*)[-*+]\s+(.*)/);
    if (ulm) {
      if (!inList || listType !== 'ul') {
        if (inList) html += closeList(listType);
        html += '<ul>';
        inList = true;
        listType = 'ul';
      }
      html += '<li>' + inlineMarkdown(ulm[2]) + '</li>';
      continue;
    }

    // ----- Ordered list -----
    const olm = line.match(/^(\s*)\d+\.\s+(.*)/);
    if (olm) {
      if (!inList || listType !== 'ol') {
        if (inList) html += closeList(listType);
        html += '<ol>';
        inList = true;
        listType = 'ol';
      }
      html += '<li>' + inlineMarkdown(olm[2]) + '</li>';
      continue;
    }

    // ----- Horizontal rule -----
    if (line.match(/^[-*_]{3,}\s*$/)) {
      if (inList) { html += closeList(listType); inList = false; }
      html += '<hr>';
      continue;
    }

    // ----- Paragraph -----
    if (inList) { html += closeList(listType); inList = false; }
    html += '<p>' + inlineMarkdown(line) + '</p>';
  }

  // Close any unclosed blocks
  if (inCodeBlock) {
    html += '<pre><code>' + esc(codeLines.join('\n')) + '</code></pre>';
  }
  if (inTable) {
    html += buildTable(tableRows);
  }
  if (inList) {
    html += closeList(listType);
  }

  return html;
}

/**
 * Render inline markdown elements to HTML.
 * Supports: bold, italic, code, links, images, strikethrough.
 * @param {string} text - Inline markdown text.
 * @returns {string} HTML string.
 */
export function inlineMarkdown(text) {
  let s = esc(text);

  // Images ![alt](url) - must be processed before links
  s = s.replace(/!\[([^\]]*)\]\(([^)]+)\)/g, function (_, alt, url) {
    const safeUrl = safeImageSource(url);
    const safeAlt = alt.replace(/"/g, '&quot;');
    if (!safeUrl) return '<span class="hc-md-link-blocked">' + safeAlt + '</span>';
    return '<img src="' + safeUrl.replace(/"/g, '&quot;') + '" alt="' + safeAlt + '" class="hc-md-img" loading="lazy">';
  });

  // Bold (**text** or __text__)
  s = s.replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>');
  s = s.replace(/__(.*?)__/g, '<strong>$1</strong>');

  // Italic (*text* or _text_)
  s = s.replace(/\*([^*]+)\*/g, '<em>$1</em>');
  s = s.replace(/(?<![a-zA-Z0-9])_([^_]+)_(?![a-zA-Z0-9])/g, '<em>$1</em>');

  // Inline code
  s = s.replace(/`([^`]+)`/g, '<code>$1</code>');

  // Links [text](url)
  s = s.replace(/\[([^\]]+)\]\(([^)]+)\)/g, function (_, label, url) {
    const safeHref = safeExternalURL(url);
    if (!safeHref) return '<span class="hc-md-link-blocked">' + label + '</span>';
    return '<a href="' + safeHref.replace(/"/g, '&quot;') + '" target="_blank" rel="noopener noreferrer">' + label + '</a>';
  });

  // Strikethrough ~~text~~
  s = s.replace(/~~(.*?)~~/g, '<del>$1</del>');

  return s;
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

function closeList(type) {
  return type === 'ul' ? '</ul>' : '</ol>';
}

function buildTable(rows) {
  if (rows.length === 0) return '';

  let html = '<table class="hc-md-table"><thead><tr>';

  // Header row
  const headerCells = parseCells(rows[0]);
  for (const cell of headerCells) {
    html += '<th>' + inlineMarkdown(cell) + '</th>';
  }
  html += '</tr></thead><tbody>';

  // Data rows (skip header, index 0)
  for (let i = 1; i < rows.length; i++) {
    const cells = parseCells(rows[i]);
    html += '<tr>';
    for (const cell of cells) {
      html += '<td>' + inlineMarkdown(cell) + '</td>';
    }
    html += '</tr>';
  }

  html += '</tbody></table>';
  return html;
}

function parseCells(row) {
  // Remove leading and trailing |, then split by |
  return row.replace(/^\|/, '').replace(/\|$/, '').split('|').map(c => c.trim());
}
