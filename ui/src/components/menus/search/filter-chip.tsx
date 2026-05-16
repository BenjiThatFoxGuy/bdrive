import { Checkbox } from "@heroui/react";

interface FilterChipProps {
  startIcon: React.ReactNode;
  children: React.ReactNode;
  value: string;
}

export const FilterChip = ({ startIcon, children, value }: FilterChipProps) => {
  return (
    <Checkbox value={value} className="rounded-full border px-3 py-1.5">
      <span className="inline-flex items-center gap-2 capitalize">
        {startIcon}
        {children}
      </span>
    </Checkbox>
  );
};
