import { memo, useEffect, useState } from "react";
import IconSvgSpinnerTadpole from "~icons/svg-spinners/tadpole";
import clsx from "clsx";

import { center } from "@/utils/classes";

interface ImagePreviewProps {
  name: string;
  assetUrl: string;
}

const ImagePreview = ({ name, assetUrl }: ImagePreviewProps) => {
  const [isLoaded, setIsLoaded] = useState<boolean>(false);

  useEffect(() => {
    setIsLoaded(false);
  }, [assetUrl]);

  const handleImageOnLoad = () => {
    setIsLoaded(true);
  };

  return (
    <div className="relative w-full h-[calc(100%-2rem)]">
      {!isLoaded && (
        <IconSvgSpinnerTadpole className={clsx(center, "size-8")} />
      )}
      <img
        key={assetUrl}
        onLoad={handleImageOnLoad}
        className={clsx(
          "opacity-0 absolute w-full h-full object-contain transition-opacity duration-300 ease-in-out",
          isLoaded && "opacity-100",
        )}
        src={assetUrl}
        alt={name}
      />
    </div>
  );
};

export default memo(ImagePreview);