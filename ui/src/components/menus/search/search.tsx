import { memo, useCallback, useEffect, useRef, useState } from "react";
import { type NavigateOptions, useRouter, useRouterState } from "@tanstack/react-router";
import {
  Button,
  Checkbox,
  CheckboxGroup,
  Input,
  Popover,
  Radio,
  RadioGroup,
} from "@heroui/react";
import IconFaSolidFile from "~icons/fa-solid/file";
import IconFaSolidFileArchive from "~icons/fa-solid/file-archive";
import IconFaSolidFileImage from "~icons/fa-solid/file-image";
import IconFaSolidFilePdf from "~icons/fa-solid/file-pdf";
import IconFa6SolidFileVideo from "~icons/fa6-solid/file-video";
import IconIcOutlineFolderOpen from "~icons/ic/outline-folder-open";
import IconIconamoonMusic1Bold from "~icons/iconamoon/music-1-bold";
import IconIcBaselineHistory from "~icons/ic/baseline-history";
import IconMaterialSymbolsClose from "~icons/material-symbols/close";
import clsx from "clsx";
import { Controller, useForm, useWatch } from "react-hook-form";
import { AnimatePresence, motion } from "framer-motion";
import { useLocalStorage } from "usehooks-ts";

import { scrollbarClasses } from "@/utils/classes";

import { FilterChip } from "./filter-chip";
import type { FileListParams } from "@/types";

const getCurrentDateFormatted = () => {
  const today = new Date();
  const year = today.getFullYear();
  const month = String(today.getMonth() + 1).padStart(2, "0");
  const day = String(today.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
};

const categories = [
  { icon: IconFaSolidFileArchive, value: "archive" },
  { icon: IconIconamoonMusic1Bold, value: "audio" },
  { icon: IconFaSolidFileImage, value: "image" },
  { icon: IconFa6SolidFileVideo, value: "video" },
  { icon: IconFaSolidFilePdf, value: "document" },
  { icon: IconIcOutlineFolderOpen, value: "folder" },
  { icon: IconFaSolidFile, value: "other" },
];

const searchTypes = ["text", "regex"];

const locations = ["current", "custom"];

const modifiedDateValues = [
  { label: "Last 7 days", value: "7" },
  { label: "Last 30 days", value: "30" },
  { label: "Last 90 days", value: "90" },
  { label: "Custom", value: "-2" },
];

interface SearchMenuProps {
  isOpen: boolean;
  setIsOpen: (open: boolean) => void;
}

const defaultFilters = {
  category: [] as string[],
  deepSearch: false,
  fromDate: "",
  location: "",
  modifiedDate: "",
  path: "",
  query: "",
  searchType: "text",
  toDate: "",
};

export const SearchMenu = memo(({ isOpen, setIsOpen }: SearchMenuProps) => {
  const formRef = useRef<HTMLFormElement | null>(null);
  const [recentSearches, setRecentSearches] = useLocalStorage<string[]>("recent-searches", []);

  const { control, handleSubmit, reset, setValue } = useForm({
    defaultValues: defaultFilters,
  });

  const modifiedDate = useWatch({ control, name: "modifiedDate" });
  const fromDate = useWatch({ control, name: "fromDate" });
  const location = useWatch({ control, name: "location" });
  const query = useWatch({ control, name: "query" });

  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const today = getCurrentDateFormatted();
  const router = useRouter();
  const [isSearching, setIsSearching] = useState(false);

  const onSubmit = useCallback(
    (data: typeof defaultFilters) => {
      const filterQuery = {} as FileListParams["params"];

      if (data.query) {
        setRecentSearches((prev) => {
          const filtered = prev.filter((s) => s !== data.query);
          return [data.query, ...filtered].slice(0, 5);
        });
      }

      for (const key in data) {
        const value = data[key];

        if (key === "category" && value.length > 0) {
          filterQuery[key] = value.join(",");
        } else if (key === "modifiedDate") {
          if (value === "-2") {
            if (data.fromDate) {
              filterQuery.updatedAt = `gte:${data.fromDate}`;
            }
            if (data.toDate) {
              filterQuery.updatedAt = filterQuery.updatedAt
                ? `${filterQuery.updatedAt},lte:${data.toDate}`
                : `lte:${data.toDate}`;
            }
          } else if (Number(value) > 0) {
            const currentDate = new Date();
            currentDate.setUTCDate(currentDate.getUTCDate() - Number(value));
            filterQuery.updatedAt = `gte:${currentDate.toISOString().split("T")[0]}`;
          }
        } else if (key === "location" && value === "current" && pathname.includes("/my-drive")) {
          const path = pathname.split("/my-drive")[1] || "/";
          filterQuery.path = decodeURI(path);
          filterQuery.deepSearch = data.deepSearch;
        } else if (key === "location" && value === "custom" && data.path) {
          filterQuery.path = data.path;
          filterQuery.deepSearch = data.deepSearch;
        } else if (key === "query" && value) {
          filterQuery[key] = value;
        } else if (key === "searchType" && value) {
          filterQuery[key] = value;
        }
      }
      const nextRoute: NavigateOptions = {
        params: {
          view: "search",
        },
        search: filterQuery,
        to: "/$view",
      };

      if (Object.keys(filterQuery).length === 0) {
        return;
      }
      setIsSearching(true);
      router
        .preloadRoute(nextRoute)
        .then(() => router.navigate(nextRoute).then(() => setIsOpen(false)))
        .finally(() => setIsSearching(false));
    },
    [pathname, setRecentSearches, setIsOpen, router],
  );

  const removeRecentSearch = (search: string) => {
    setRecentSearches((prev) => prev.filter((s) => s !== search));
  };

  return (
    <Popover
      isOpen={isOpen}
      onOpenChange={(open) => setIsOpen(open)}
    >
      <div />
      <Popover.Content className="max-w-md max-h-[80vh] justify-normal p-0 rounded-2xl">
        <form
          ref={formRef}
          id="filter-form"
          onSubmit={handleSubmit(onSubmit)}
          className={clsx(
            "flex flex-col gap-6 p-6 w-full overflow-y-auto",
            scrollbarClasses,
          )}
        >
          {recentSearches.length > 0 && !query && (
            <section className="flex flex-col gap-2">
              <h3 className="text-sm font-medium text-muted flex items-center gap-2">
                <IconIcBaselineHistory className="size-4" />
                Recent Searches
              </h3>
              <div className="flex flex-wrap gap-2">
                {recentSearches.map((s) => (
                  <div
                    key={s}
                    className="group flex items-center gap-1 bg-surface-secondary hover:bg-surface-secondary transition-colors rounded-full pl-3 pr-1 py-1 cursor-pointer"
                    onClick={() => {
                      setValue("query", s);
                      handleSubmit(onSubmit)();
                    }}
                  >
                    <span className="text-sm">{s}</span>
                    <Button
                      isIconOnly
                      variant="ghost"
                      size="sm"
                      className="size-5 min-w-5 p-0 opacity-0 group-hover:opacity-100 transition-opacity"
                      onPress={(e) => {
                        (e as any).stopPropagation?.();
                        removeRecentSearch(s);
                      }}
                    >
                      <IconMaterialSymbolsClose className="size-3" />
                    </Button>
                  </div>
                ))}
              </div>
            </section>
          )}

          <section className="flex flex-col gap-4">
            <h3 className="text-sm font-medium text-muted">Keywords</h3>
            <Controller
              name="query"
              control={control}
              render={({ field }) => (
                <Input
                  placeholder="Filename or regex..."
                  autoComplete="off"
                  autoFocus

                  {...field}
                />
              )}
            />
            <Controller
              control={control}
              name="searchType"
              render={({ field }) => (
                <RadioGroup
                  className="gap-6"
                  orientation="horizontal"
                  {...field}
                >
                  {searchTypes.map((type) => (
                    <Radio
                      value={type}
                      key={type}
                      className="capitalize text-sm"
                    >
                      {type === "text" ? "Exact Match" : "Regular Expression"}
                    </Radio>
                  ))}
                </RadioGroup>
              )}
            />
          </section>

          <section className="flex flex-col gap-3">
            <h3 className="text-sm font-medium text-muted">Categories</h3>
            <Controller
              control={control}
              name="category"
              render={({ field }) => (
                <CheckboxGroup
                  className="flex flex-wrap gap-2"
                  {...field}
                >
                  {categories.map((category) => (
                    <FilterChip
                      startIcon={<category.icon className="size-5 max-h-none" />}
                      value={category.value}
                      key={category.value}
                    >
                      {category.value}
                    </FilterChip>
                  ))}
                </CheckboxGroup>
              )}
            />
          </section>

          <section className="flex flex-col gap-3">
            <h3 className="text-sm font-medium text-muted">Location</h3>
            <div className="flex flex-col gap-4">
              <div className="flex items-center justify-between">
                <Controller
                  control={control}
                  name="location"
                  render={({ field }) => (
                    <RadioGroup
                      className="gap-6"
                      orientation="horizontal"
                      {...field}
                    >
                      {locations.map((loc) => (
                        <Radio
                          value={loc}
                          key={loc}
                          className="capitalize text-sm"
                        >
                          {loc === "current" ? "Current Folder" : "Everywhere"}
                        </Radio>
                      ))}
                    </RadioGroup>
                  )}
                />
                <Controller
                  name="deepSearch"
                  control={control}
                  render={({ field }) => (
                    <Checkbox
                      onChange={field.onChange}
                      isSelected={field.value}
                      className="text-sm"
                      name={field.name}
                    >
                      Include Subfolders
                    </Checkbox>
                  )}
                />
              </div>
            </div>
          </section>

          <section className="flex flex-col gap-3">
            {modifiedDate === "-2" && (
              <div className="flex flex-col gap-2">
                <Controller
                  name="fromDate"
                  control={control}
                  render={({ field }) => (
                    <Input
                      type="date"
                      max={today}
                      {...field}
                    />
                  )}
                />
                <Controller
                  name="toDate"
                  control={control}
                  render={({ field }) => (
                    <Input
                      type="date"max={today}
                      {...field}
                    />
                  )}
                />
              </div>
            )}
            {location === "custom" && (
              <Controller
                name="path"
                control={control}
                render={() => (
                  <Input
                    placeholder="/path/to/folder"/>
                )}
              />
            )}
          </section>
        </form>
      </Popover.Content>
    </Popover>
  );
});
