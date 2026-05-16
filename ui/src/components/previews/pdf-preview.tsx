import { memo } from "react";

const PDFEmbedPreview = ({ assetUrl }: { assetUrl: string }) => {
  const url = `https://pdfview.pages.dev/web/viewer.html?file=${assetUrl}`;
  return (
    <iframe aria-label="PdfView"
      className="relative border-none z-50 size-full"
      src={url}
      allowFullScreen
    />
  );
};

export default memo(PDFEmbedPreview);
