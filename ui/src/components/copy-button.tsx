import { useCallback, useState } from "react";
import { Button, type ButtonRootProps } from "@heroui/react";
import CheckLinearIcon from "~icons/ic/round-check";
import CopyLinearIcon from "~icons/mingcute/copy-2-line";

export type CopyButtonProps = ButtonRootProps & {
  value?: string;
};

export const CopyButton = ({ value, ...buttonProps }: CopyButtonProps) => {
  const [copied, setCopied] = useState(false);

  const handleCopy = useCallback(() => {
    if (!value) return;
    navigator.clipboard.writeText(value).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }, [value]);

  return (
    <Button
      isIconOnly
      className="z-50 border-1 before:content-[''] before:block before:z-[-1] before:absolute before:inset-0"
      variant="secondary"
      onPress={handleCopy}
      {...buttonProps}
    >
      <CheckLinearIcon
        className="absolute size-6 opacity-0 scale-50  data-[visible=true]:opacity-100 data-[visible=true]:scale-100 transition-transform-opacity"
        data-visible={copied}
      />
      <CopyLinearIcon
        className="absolute size-6 opacity-0 scale-50 data-[visible=true]:opacity-100 data-[visible=true]:scale-100 transition-transform-opacity"
        data-visible={!copied}
      />
    </Button>
  );
};
