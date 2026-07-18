# Contributing to BDrive

This guide will help you get started with contributing to BDrive.

## Development Setup

### Prerequisites

- Go (1.22 or later)
- Git
- Task
- PowerShell (for Windows) or Bash (for Unix-like systems)

### Install Task

##### macOS/Linux (curl)
```sh
curl https://instl.vercel.app/go-task/task | bash
```

#### PowerShell/cmd.exe
```powershell
powershell -c "irm https://instl.vercel.app/go-task/task?platform=windows|iex"
```

### Initial Setup

1. Clone the repository:
```bash
git clone https://github.com/BenjiThatFoxGuy/bdrive.git
cd bdrive
```

2. Install dependencies:
```bash
task deps
```

## Building BDrive

### Complete Build
To build both frontend and backend:
```bash
task
```

### Frontend Development
The frontend is managed in a separate repository ([bdrive-ui](https://github.com/BenjiThatFoxGuy/bdrive-ui)). The main repository pulls the latest frontend release during build.

To set up the frontend:
```bash
task ui
```

### Backend Development
To build the backend only:
```bash
task server
```

### Running BDrive
After building, run the application:
```bash
task run
```

## Feature Development

1. Create a new branch for your feature:
```bash
git checkout -b feature/your-feature-name
```

2. Generate API Spec:
```bash
task gen
```

## Pull Request Guidelines

1. **Branch Naming**:
   - `feature/` for new features
   - `fix/` for bug fixes
   - `docs/` for documentation changes
   - `refactor/` for code refactoring

2. **Commit Messages**:
   - Use clear, descriptive commit messages
   - Reference issues when applicable

3. **Pull Request Description**:
   - Describe the changes made
   - Include any relevant issue numbers
   - List any breaking changes

## Important: Deduplication Feature

BDrive implements **file-level deduplication** to reduce storage usage. If you're working on:

- **File upload/modification**: See [docs/DEDUPLICATION.md](docs/DEDUPLICATION.md) for architecture and service functions
- **Admin tools**: Use the `teldrive deduplicate` command for retroactive deduplication
- **Frontend (bdrive-ui)**: Implement hash parameter in upload flow
- **rclone-connector**: Add hash-based dedup detection before upload

**Key Points:**
- Only non-encrypted files are deduplicated
- Deduplication uses copy-on-write semantics
- `ReferencedFileId` field links duplicate files to their canonical copy
- Dependent repos (bdrive-ui, rclone-connector) need updates to take advantage of dedup

See [docs/DEDUPLICATION.md](docs/DEDUPLICATION.md) for complete implementation details and handoff notes for dependent repositories.