IF DB_ID('appdb') IS NULL
BEGIN
    CREATE DATABASE appdb;
END
GO

USE appdb;
GO

-- Users must exist before Orders/RefreshTokens, since both carry foreign
-- keys into it.
IF OBJECT_ID('dbo.Users', 'U') IS NULL
BEGIN
    CREATE TABLE dbo.Users (
        ID           VARCHAR(36)   NOT NULL PRIMARY KEY,
        Username     VARCHAR(64)   NOT NULL,
        PasswordHash VARCHAR(255)  NOT NULL, -- bcrypt
        IsActive     BIT           NOT NULL CONSTRAINT DF_Users_IsActive DEFAULT (1),
        CreatedAt    DATETIME2     NOT NULL
    );

    CREATE UNIQUE INDEX UX_Users_Username ON dbo.Users (Username);
END
GO

IF OBJECT_ID('dbo.RefreshTokens', 'U') IS NULL
BEGIN
    CREATE TABLE dbo.RefreshTokens (
        ID         VARCHAR(36)   NOT NULL PRIMARY KEY,
        UserID     VARCHAR(36)   NOT NULL CONSTRAINT FK_RefreshTokens_Users REFERENCES dbo.Users(ID),
        TokenHash  CHAR(64)      NOT NULL, -- hex-encoded SHA-256; raw token is never stored
        ExpiresAt  DATETIME2     NOT NULL,
        RevokedAt  DATETIME2     NULL,
        ReplacedBy VARCHAR(36)   NULL CONSTRAINT FK_RefreshTokens_ReplacedBy REFERENCES dbo.RefreshTokens(ID),
        CreatedAt  DATETIME2     NOT NULL
    );

    CREATE UNIQUE INDEX UX_RefreshTokens_TokenHash ON dbo.RefreshTokens (TokenHash);

    -- Filtered to still-valid tokens: this is the index RevokeAllForUser
    -- and the rotation-chain lookups actually hit.
    CREATE INDEX IX_RefreshTokens_UserID_Active
        ON dbo.RefreshTokens (UserID)
        WHERE RevokedAt IS NULL;
END
GO

IF OBJECT_ID('dbo.Orders', 'U') IS NULL
BEGIN
    CREATE TABLE dbo.Orders (
        ID           VARCHAR(36)   NOT NULL PRIMARY KEY,
        CustomerID   VARCHAR(64)   NOT NULL,
        Status       VARCHAR(32)   NOT NULL,
        Total        DECIMAL(18,2) NOT NULL,
        CurrencyCode CHAR(3)       NOT NULL CONSTRAINT DF_Orders_CurrencyCode DEFAULT ('USD'),
        CreatedAt    DATETIME2     NOT NULL,
        UpdatedAt    DATETIME2     NULL,
        IsDeleted    BIT           NOT NULL CONSTRAINT DF_Orders_IsDeleted DEFAULT (0),
        CreatedBy    VARCHAR(36)   NOT NULL CONSTRAINT FK_Orders_CreatedBy REFERENCES dbo.Users(ID),
        UpdatedBy    VARCHAR(36)   NOT NULL CONSTRAINT FK_Orders_UpdatedBy REFERENCES dbo.Users(ID)
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

-- ============================== Users ==============================

CREATE OR ALTER PROCEDURE dbo.usp_User_GetByUsername
    @Username VARCHAR(64)
AS
BEGIN
    SET NOCOUNT ON;

    -- Deliberately does NOT filter IsActive here: AuthService needs to
    -- see an inactive user in order to reject the login, and treats that
    -- rejection identically to "wrong password" so neither is leaked to
    -- the caller.
    SELECT ID, Username, PasswordHash, IsActive, CreatedAt
    FROM dbo.Users
    WHERE Username = @Username;
END
GO

-- Used by GET /users/me — the JWT carries a user ID (subject claim), not
-- a username, so profile lookups go through ID rather than
-- usp_User_GetByUsername.
CREATE OR ALTER PROCEDURE dbo.usp_User_GetByID
    @ID VARCHAR(36)
AS
BEGIN
    SET NOCOUNT ON;

    SELECT ID, Username, PasswordHash, IsActive, CreatedAt
    FROM dbo.Users
    WHERE ID = @ID;
END
GO

-- Insert-or-reset-password by username. Re-runnable, so cmd/seeduser can
-- be used both to create the first account and to rotate a dev password
-- later without erroring on a duplicate username.
-- Insert-or-reset-password by username. Re-runnable, so cmd/seeduser can
-- be used both to create the first account and to rotate a dev password
-- later without erroring on a duplicate username.
--
-- The existence check runs WITH (UPDLOCK, HOLDLOCK) inside an explicit
-- transaction: under the default READ COMMITTED isolation, a plain
-- IF EXISTS / ELSE INSERT is a check-then-act race — two concurrent calls
-- for the same @Username can both see "not exists" before either commits,
-- and both attempt the INSERT branch (which UX_Users_Username's unique
-- index would then reject with a duplicate-key error on whichever commits
-- second, rather than silently duplicating the row). UPDLOCK+HOLDLOCK
-- takes and holds a key-level lock on this username for the transaction's
-- duration, so a second concurrent call for the SAME username blocks at
-- the check until the first commits, instead of racing past it. Different
-- usernames are unaffected — the lock is scoped to the key being checked.
CREATE OR ALTER PROCEDURE dbo.usp_User_Upsert
    @ID           VARCHAR(36),
    @Username     VARCHAR(64),
    @PasswordHash VARCHAR(255),
    @CreatedAt    DATETIME2
AS
BEGIN
    SET NOCOUNT ON;
    SET XACT_ABORT ON;

    BEGIN TRANSACTION;

    IF EXISTS (
        SELECT 1 FROM dbo.Users WITH (UPDLOCK, HOLDLOCK)
        WHERE Username = @Username
    )
    BEGIN
        UPDATE dbo.Users
        SET PasswordHash = @PasswordHash,
            IsActive     = 1
        WHERE Username = @Username;
    END
    ELSE
    BEGIN
        INSERT INTO dbo.Users (ID, Username, PasswordHash, IsActive, CreatedAt)
        VALUES (@ID, @Username, @PasswordHash, 1, @CreatedAt);
    END

    COMMIT TRANSACTION;
END
GO

-- ========================== RefreshTokens ==========================

CREATE OR ALTER PROCEDURE dbo.usp_RefreshToken_Create
    @ID        VARCHAR(36),
    @UserID    VARCHAR(36),
    @TokenHash CHAR(64),
    @ExpiresAt DATETIME2,
    @CreatedAt DATETIME2
AS
BEGIN
    SET NOCOUNT ON;

    INSERT INTO dbo.RefreshTokens (ID, UserID, TokenHash, ExpiresAt, RevokedAt, ReplacedBy, CreatedAt)
    VALUES (@ID, @UserID, @TokenHash, @ExpiresAt, NULL, NULL, @CreatedAt);
END
GO

-- Deliberately does NOT filter out revoked tokens: AuthService.Refresh
-- needs to see a revoked token to detect reuse (a strong signal of
-- theft), not just be told "not found".
CREATE OR ALTER PROCEDURE dbo.usp_RefreshToken_GetByTokenHash
    @TokenHash CHAR(64)
AS
BEGIN
    SET NOCOUNT ON;

    SELECT ID, UserID, TokenHash, ExpiresAt, RevokedAt, ReplacedBy, CreatedAt
    FROM dbo.RefreshTokens
    WHERE TokenHash = @TokenHash;
END
GO

CREATE OR ALTER PROCEDURE dbo.usp_RefreshToken_Revoke
    @ID         VARCHAR(36),
    @ReplacedBy VARCHAR(36) = NULL,
    @RevokedAt  DATETIME2
AS
BEGIN
    SET NOCOUNT ON;

    UPDATE dbo.RefreshTokens
    SET RevokedAt  = @RevokedAt,
        ReplacedBy = @ReplacedBy
    WHERE ID = @ID
      AND RevokedAt IS NULL;
END
GO

CREATE OR ALTER PROCEDURE dbo.usp_RefreshToken_RevokeAllForUser
    @UserID    VARCHAR(36),
    @RevokedAt DATETIME2
AS
BEGIN
    SET NOCOUNT ON;

    UPDATE dbo.RefreshTokens
    SET RevokedAt = @RevokedAt
    WHERE UserID = @UserID
      AND RevokedAt IS NULL;
END
GO

-- ============================== Orders ==============================

CREATE OR ALTER PROCEDURE dbo.usp_Order_Create
    @ID           VARCHAR(36),
    @CustomerID   VARCHAR(64),
    @Status       VARCHAR(32),
    @Total        DECIMAL(18,2),
    @CurrencyCode CHAR(3),
    @CreatedAt    DATETIME2,
    @CreatedBy    VARCHAR(36),
    @UpdatedBy    VARCHAR(36)
AS
BEGIN
    SET NOCOUNT ON;

    INSERT INTO dbo.Orders (ID, CustomerID, Status, Total, CurrencyCode, CreatedAt, UpdatedAt, IsDeleted, CreatedBy, UpdatedBy)
    VALUES (@ID, @CustomerID, @Status, @Total, @CurrencyCode, @CreatedAt, NULL, 0, @CreatedBy, @UpdatedBy);
END
GO

CREATE OR ALTER PROCEDURE dbo.usp_Order_GetByID
    @ID VARCHAR(36)
AS
BEGIN
    SET NOCOUNT ON;

    SELECT ID, CustomerID, Status, Total, CurrencyCode, CreatedAt, UpdatedAt, IsDeleted, CreatedBy, UpdatedBy
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
        CurrencyCode,
        CreatedAt,
        UpdatedAt,
        IsDeleted,
        CreatedBy,
        UpdatedBy,
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
-- CreatedBy is never touched; UpdatedBy always reflects the acting user.
CREATE OR ALTER PROCEDURE dbo.usp_Order_Update
    @ID           VARCHAR(36),
    @CustomerID   VARCHAR(64),
    @Status       VARCHAR(32),
    @Total        DECIMAL(18,2),
    @CurrencyCode CHAR(3),
    @UpdatedBy    VARCHAR(36),
    @UpdatedAt    DATETIME2
AS
BEGIN
    SET NOCOUNT ON;

    UPDATE dbo.Orders
    SET CustomerID   = @CustomerID,
        Status        = @Status,
        Total         = @Total,
        CurrencyCode  = @CurrencyCode,
        UpdatedBy     = @UpdatedBy,
        UpdatedAt     = @UpdatedAt
    WHERE ID = @ID
      AND IsDeleted = 0;

    SELECT ID, CustomerID, Status, Total, CurrencyCode, CreatedAt, UpdatedAt, IsDeleted, CreatedBy, UpdatedBy
    FROM dbo.Orders
    WHERE ID = @ID
      AND IsDeleted = 0;
END
GO

-- Logical delete only: flags IsDeleted, never physically removes the row,
-- so order history survives for audit/reporting. A delete is still a
-- modification of the row, so UpdatedBy is set to the acting user just
-- like on a regular update. Returns the number of rows the UPDATE
-- actually touched so the caller can tell "deleted" apart from "already
-- gone / never existed".
CREATE OR ALTER PROCEDURE dbo.usp_Order_Delete
    @ID        VARCHAR(36),
    @UpdatedBy VARCHAR(36),
    @DeletedAt DATETIME2
AS
BEGIN
    SET NOCOUNT ON;

    UPDATE dbo.Orders
    SET IsDeleted = 1,
        UpdatedBy = @UpdatedBy,
        UpdatedAt = @DeletedAt
    WHERE ID = @ID
      AND IsDeleted = 0;

    SELECT CAST(@@ROWCOUNT AS INT) AS RowsAffected;
END
GO
