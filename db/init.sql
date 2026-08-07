IF DB_ID('appdb') IS NULL
BEGIN
    CREATE DATABASE appdb;
END
GO

USE appdb;
GO

IF OBJECT_ID('dbo.Orders', 'U') IS NULL
BEGIN
    CREATE TABLE dbo.Orders (
        ID          VARCHAR(36)   NOT NULL PRIMARY KEY,
        CustomerID  VARCHAR(64)   NOT NULL,
        Status      VARCHAR(32)   NOT NULL,
        Total       DECIMAL(18,2) NOT NULL,
        CreatedAt   DATETIME2     NOT NULL,
        UpdatedAt   DATETIME2     NULL,
        IsDeleted   BIT           NOT NULL CONSTRAINT DF_Orders_IsDeleted DEFAULT (0)
    );

    -- Both indexes are filtered to IsDeleted = 0: they only index "live"
    -- rows, which is what List/GetByID/Update always query, and keeps the
    -- indexes smaller as soft-deleted history accumulates.
    CREATE INDEX IX_Orders_CustomerID_CreatedAt
        ON dbo.Orders (CustomerID, CreatedAt DESC)
        WHERE IsDeleted = 0;

    CREATE INDEX IX_Orders_IsDeleted_CreatedAt
        ON dbo.Orders (IsDeleted, CreatedAt DESC);
END
GO

CREATE OR ALTER PROCEDURE dbo.usp_Order_Create
    @ID         VARCHAR(36),
    @CustomerID VARCHAR(64),
    @Status     VARCHAR(32),
    @Total      DECIMAL(18,2),
    @CreatedAt  DATETIME2
AS
BEGIN
    SET NOCOUNT ON;

    INSERT INTO dbo.Orders (ID, CustomerID, Status, Total, CreatedAt, UpdatedAt, IsDeleted)
    VALUES (@ID, @CustomerID, @Status, @Total, @CreatedAt, NULL, 0);
END
GO

CREATE OR ALTER PROCEDURE dbo.usp_Order_GetByID
    @ID VARCHAR(36)
AS
BEGIN
    SET NOCOUNT ON;

    SELECT ID, CustomerID, Status, Total, CreatedAt, UpdatedAt, IsDeleted
    FROM dbo.Orders
    WHERE ID = @ID
      AND IsDeleted = 0;
END
GO

-- Pages through non-deleted orders, newest first, optionally filtered by
-- CustomerID (NULL = no filter). TotalCount rides along on every returned
-- row via COUNT(*) OVER(), so the caller gets the page AND the total
-- matching row count in a single round trip instead of a separate
-- SELECT COUNT(*) query.
CREATE OR ALTER PROCEDURE dbo.usp_Order_List
    @CustomerID VARCHAR(64) = NULL,
    @PageNumber INT         = 1,
    @PageSize   INT         = 20
AS
BEGIN
    SET NOCOUNT ON;

    IF @PageNumber IS NULL OR @PageNumber < 1 SET @PageNumber = 1;
    IF @PageSize   IS NULL OR @PageSize   < 1 SET @PageSize   = 20;
    IF @PageSize > 200 SET @PageSize = 200;

    SELECT
        ID,
        CustomerID,
        Status,
        Total,
        CreatedAt,
        UpdatedAt,
        IsDeleted,
        COUNT(*) OVER() AS TotalCount
    FROM dbo.Orders
    WHERE IsDeleted = 0
      AND (@CustomerID IS NULL OR CustomerID = @CustomerID)
    ORDER BY CreatedAt DESC, ID
    OFFSET (@PageNumber - 1) * @PageSize ROWS
    FETCH NEXT @PageSize ROWS ONLY;
END
GO

-- Updates and re-selects the row in one round trip. The WHERE clause
-- excludes soft-deleted rows, so this is a harmless no-op — 0 rows
-- updated, 0 rows returned — for both a missing ID and an already-deleted
-- one. The Go repository treats an empty result set as "not found".
CREATE OR ALTER PROCEDURE dbo.usp_Order_Update
    @ID         VARCHAR(36),
    @CustomerID VARCHAR(64),
    @Status     VARCHAR(32),
    @Total      DECIMAL(18,2),
    @UpdatedAt  DATETIME2
AS
BEGIN
    SET NOCOUNT ON;

    UPDATE dbo.Orders
    SET CustomerID = @CustomerID,
        Status      = @Status,
        Total       = @Total,
        UpdatedAt   = @UpdatedAt
    WHERE ID = @ID
      AND IsDeleted = 0;

    SELECT ID, CustomerID, Status, Total, CreatedAt, UpdatedAt, IsDeleted
    FROM dbo.Orders
    WHERE ID = @ID
      AND IsDeleted = 0;
END
GO

-- Logical delete only: flags IsDeleted, never physically removes the row,
-- so order history survives for audit/reporting. Returns the number of
-- rows the UPDATE actually touched so the caller can tell "deleted" apart
-- from "already gone / never existed".
CREATE OR ALTER PROCEDURE dbo.usp_Order_Delete
    @ID        VARCHAR(36),
    @DeletedAt DATETIME2
AS
BEGIN
    SET NOCOUNT ON;

    UPDATE dbo.Orders
    SET IsDeleted = 1,
        UpdatedAt = @DeletedAt
    WHERE ID = @ID
      AND IsDeleted = 0;

    SELECT CAST(@@ROWCOUNT AS INT) AS RowsAffected;
END
GO
