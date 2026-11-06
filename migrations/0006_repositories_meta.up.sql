ALTER TABLE repositories
  ADD COLUMN default_branch VARCHAR(255) NOT NULL DEFAULT 'main',
  ADD COLUMN provider ENUM('github','git') NOT NULL DEFAULT 'github';
