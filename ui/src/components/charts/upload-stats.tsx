import type { SetValue, UploadStats } from "@/types";
import { Dropdown } from "@heroui/react";
import { Button } from "@heroui/react";
import type { ApexOptions } from "apexcharts";
import { memo, useMemo } from "react";
import ReactApexChart from "react-apexcharts";

const options: ApexOptions = {
  legend: {
    show: false,
  },
  colors: ["oklch(var(--color-accent))"],
  chart: {
    height: 250,
    type: "area",
    fontFamily: "Rubik, sans-serif",
    toolbar: {
      show: false,
    },
    zoom: {
      enabled: false,
    },
    sparkline: {
      enabled: false,
    },
  },
  fill: {
    type: "gradient",
    gradient: {
      shadeIntensity: 1,
      opacityFrom: 0.45,
      opacityTo: 0.05,
      stops: [20, 100, 100, 100],
    },
  },
  stroke: {
    width: 3,
    curve: "smooth",
  },
  grid: {
    show: true,
    borderColor: "var(--color-border)",
    strokeDashArray: 4,
    padding: {
      left: 20,
      right: 20,
      bottom: 0,
    },
  },
  dataLabels: {
    enabled: false,
  },
  markers: {
    size: 5,
    colors: ["oklch(var(--color-accent))"],
    strokeColors: "var(--color-surface)",
    strokeWidth: 2,
    hover: {
      size: 7,
    },
  },
  xaxis: {
    type: "category",
    axisBorder: {
      show: false,
    },
    axisTicks: {
      show: false,
    },
    labels: {
      style: {
        colors: "var(--color-muted)",
        fontSize: "12px",
        fontWeight: 500,
      },
    },
  },
  yaxis: {
    labels: {
      style: {
        colors: "var(--color-muted)",
        fontSize: "12px",
        fontWeight: 500,
      },
    },
  },
  tooltip: {
    theme: "dark",
    x: {
      show: true,
    },
    y: {
      formatter: (val) => `${val.toFixed(2)} GB`,
    },
    style: {
      fontSize: "12px",
      fontFamily: "Rubik, sans-serif",
    },
  },
};

function getChartData(stats: UploadStats[]): ApexOptions {
  const categories = stats.map((stat) => stat.uploadDate);
  const data = stats.map((stat) => stat.totalUploaded);
  return {
    ...options,
    xaxis: {
      ...options.xaxis,
      categories,
    },
    series: [
      {
        name: "Uploaded",
        data,
      },
    ],
  };
}

interface UploadStatsChartProps {
  stats: UploadStats[];
  days: number;
  setDays: SetValue<number>;
}

const allowedDays = [7, 15, 30, 60];

export const UploadStatsChart = memo(
  ({ stats, days, setDays }: UploadStatsChartProps) => {
    const chartOptions = useMemo(() => getChartData(stats), [stats]);

    return (
      <div className="w-full">
        <div className="flex justify-end mb-2">
          <Dropdown>
            <Dropdown.Trigger>
              <Button
                variant="secondary"
                className="rounded-xl px-4 py-2 font-medium bg-accent-soft text-accent-soft-foreground"
              >{`${days} Days`}</Button>
            </Dropdown.Trigger>
            <Dropdown.Popover className="min-w-32">
              <Dropdown.Menu>
                {allowedDays.map((day) => (
                  <Dropdown.Item key={day} onPress={() => setDays(day)}>
                    {`${day} Days`}
                  </Dropdown.Item>
                ))}
              </Dropdown.Menu>
            </Dropdown.Popover>
          </Dropdown>
        </div>
        <div className="min-h-[250px]">
          <ReactApexChart
            options={chartOptions}
            series={chartOptions.series}
            type="area"
            height={250}
          />
        </div>
      </div>
    );
  },
);
