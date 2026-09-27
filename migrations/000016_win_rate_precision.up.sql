-- win_rate is a 0..100 percentage; NUMERIC(8,6) overflows at exactly 100.
ALTER TABLE wallet_metric_snapshots
    ALTER COLUMN win_rate TYPE NUMERIC(9,6);
