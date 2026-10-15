module Feature.Import.State exposing
    ( Access
    , Checkpoint
    , CheckpointEntry
    , CheckpointPhase(..)
    , Counts
    , DuplicatePolicy(..)
    , Effect(..)
    , Entry
    , Event(..)
    , FileFact
    , ImportOutcome(..)
    , ImportSuccess
    , Model
    , Note(..)
    , Phase(..)
    , SaveStatus(..)
    , SavedSummary
    , buildCheckpoint
    , canStart
    , canonicalApiBase
    , checkpointTotalBytes
    , isCanonicalPostId
    , isPickBlocked
    , isTerminal
    , maxTotalBytes
    , restoreCheckpoint
    , setOpen
    , classify
    , counts
    , init
    , isValidHash
    , maxSupported
    , phaseLabel
    , policyFromString
    , policyString
    , resume
    , update
    )

{-| Pure folder-import preview and sequential import state.

Picking files only classifies, hashes, and looks up duplicates: no
`BeginImport` effect exists before an explicit `StartRequested`.
Hashing and importing are each concurrency 1. File bytes never enter
this module; only plain facts, hashes, and outcomes cross the boundary.
-}

import Api.Import as ImportApi
import Domain.Post exposing (PostDetail)
import Domain.UploadRules as UploadRules
import Feature.UploadQueue.Queue as Queue
import Set exposing (Set)


maxSupported : Int
maxSupported =
    1000


{-| Aggregate byte bound for one saved (durable) import: 250 MiB. -}
maxTotalBytes : Int
maxTotalBytes =
    250 * 1024 * 1024


checkpointSchema : Int
checkpointSchema =
    1


{-| Canonical identity of an API base for saved-import matching:
trimmed, no trailing slash, lower-cased scheme and authority. A saved
import is only ever resumed against the exact canonical base it was
saved for; anything else is rejected, never redirected.
-}
canonicalApiBase : String -> String
canonicalApiBase raw =
    let
        trimmed =
            String.trim raw |> stripTrailingSlashes
    in
    case String.split "://" trimmed of
        scheme :: rest :: more ->
            let
                authorityAndPath =
                    String.join "://" (rest :: more)

                ( authority, path ) =
                    case String.split "/" authorityAndPath of
                        head :: tail ->
                            ( head, String.join "/" tail )

                        [] ->
                            ( authorityAndPath, "" )
            in
            String.toLower scheme
                ++ "://"
                ++ String.toLower authority
                ++ (if path == "" then "" else "/" ++ path)

        _ ->
            trimmed


stripTrailingSlashes : String -> String
stripTrailingSlashes value =
    if String.endsWith "/" value && String.length value > 1 then
        stripTrailingSlashes (String.dropRight 1 value)

    else
        value


type DuplicatePolicy
    = SkipDuplicates
    | ErrorOnDuplicates


policyString : DuplicatePolicy -> String
policyString policy =
    case policy of
        SkipDuplicates ->
            "skip"

        ErrorOnDuplicates ->
            "error"


policyFromString : String -> DuplicatePolicy
policyFromString value =
    if value == "error" then
        ErrorOnDuplicates

    else
        SkipDuplicates


type alias FileFact =
    { name : String
    , relativePath : String
    , size : Int
    , mime : String
    , lastModified : Int
    }


type Phase
    = Excluded String
    | PendingHash
    | Hashing
    | Hashed
    | HashFailed String
    | QueuedImport
    | Importing Int
    | Imported { postId : String, duplicate : Bool, attached : Bool, attachmentError : Maybe String }
    | Skipped String
    | Failed String
    | Uncertain String


type alias Entry =
    { id : Int
    , name : String
    , relativePath : String
    , size : Int
    , mime : String
    , lastModified : Int
    , hash : Maybe String
    , duplicatePostId : Maybe String
    , tags : List String
    , source : String
    , artist : String
    , phase : Phase
    }


type alias Model =
    { open : Bool
    , entries : List Entry
    , generation : Int
    , hashingId : Maybe Int
    , importingId : Maybe Int
    , started : Bool
    , held : Bool
    , tagsDraft : String
    , artistDraft : String
    , sourcePrefix : String
    , collectionId : Maybe String
    , duplicatePolicy : DuplicatePolicy
    , capturedDuplicatePolicy : DuplicatePolicy
    , capturedCollectionId : Maybe String
    , overflow : Int
    , status : Maybe String
    , lookedUp : Set String
    , durableEnabled : Bool
    , saveStatus : SaveStatus
    , checkpointSeq : Int
    , pendingBegin : Maybe Int
    , savedSummary : Maybe SavedSummary
    , savedCheckpointId : Maybe Int
    , replaceNeedsConfirm : Bool
    , awaitingResume : Bool
    }


{-| Honest durable-save state. `SaveStored` is only reached after the
storage transaction commits; a requested save is `SavePending` until
then and a failed one is `SaveFailed`, never `SaveStored`.
-}
type SaveStatus
    = MemoryOnly
    | SavePending Int
    | SaveStored Int
    | SaveFailed String


type alias SavedSummary =
    { checkpointId : Int
    , fileCount : Int
    , totalBytes : Int
    , apiBase : String
    , started : Bool
    , generation : Int
    }


{-| Plain-data checkpoint phase. Carries no credential, token, access
state, or object URL by construction.
-}
type CheckpointPhase
    = CpExcluded String
    | CpPendingHash
    | CpHashing
    | CpHashed
    | CpHashFailed String
    | CpQueuedImport
    | CpImporting
    | CpImported { postId : String, duplicate : Bool, attached : Bool, attachmentError : Maybe String }
    | CpSkipped String
    | CpFailed String
    | CpUncertain String


type alias CheckpointEntry =
    { id : Int
    , name : String
    , relativePath : String
    , size : Int
    , mime : String
    , lastModified : Int
    , hash : Maybe String
    , duplicatePostId : Maybe String
    , tags : List String
    , source : String
    , artist : String
    , phase : CheckpointPhase
    , hasBlob : Bool
    , hashVerified : Bool
    }


type alias Checkpoint =
    { schema : Int
    , checkpointId : Int
    , apiBase : String
    , generation : Int
    , started : Bool
    , tagsDraft : String
    , artistDraft : String
    , sourcePrefix : String
    , collectionId : Maybe String
    , duplicatePolicy : String
    , capturedDuplicatePolicy : String
    , capturedCollectionId : Maybe String
    , overflow : Int
    , entries : List CheckpointEntry
    }


type alias Access =
    { writesEnabled : Bool
    , accessGeneration : Int
    }


type alias ImportSuccess =
    { post : PostDetail
    , duplicate : Bool
    , collectionId : Maybe String
    , attached : Bool
    , attachmentError : Maybe String
    }


type ImportOutcome
    = ImportSucceeded ImportSuccess
    | ImportFailed { message : String, authRejected : Bool, authStatus : Int }
    | ImportUncertain String


type Event
    = PanelOpened
    | PanelClosed
    | TagsEdited String
    | ArtistEdited String
    | SourcePrefixEdited String
    | CollectionSelected (Maybe String)
    | PolicySelected DuplicatePolicy
    | PickFailed String
    | FilesChosen Int (List FileFact)
    | HashSucceeded { entryId : Int, generation : Int, hash : String, size : Int, lastModified : Int }
    | HashErrored { entryId : Int, generation : Int, message : String }
    | DuplicatesArrived { generation : Int, matches : List ImportApi.DuplicateMatch }
    | DuplicatesFailed { generation : Int, message : String }
    | StartRequested
    | ImportSettled { entryId : Int, generation : Int, outcome : ImportOutcome }
    | DismissRequested Int
    | FinishedCleared
    | DurableEnabledChanged Bool
    | ReplaceSavedConfirmed
    | CheckpointStored Int
    | CheckpointFailed Int String
    | SavedProbeArrived (Maybe SavedSummary)
    | SavedProbeFailed String
    | RestoreSavedRequested
    | SavedRestored String Checkpoint
    | SavedRestoreFailed String
    | DiscardSavedRequested
    | SavedDiscarded
    | SavedDiscardFailed String
    | ExplicitResumeRequested


type Effect
    = HashEntry Int Int
    | LookupHashes Int (List String)
    | BeginImport Int
    | ReleaseFile Int
    | ReleaseFiles (List Int)
    | SaveCheckpoint Int
    | DeleteSavedCheckpoint
    | RestoreSavedCheckpoint


type Note
    = ImportConfirmed ImportSuccess
    | CredentialRejected Int Int


init : Model
init =
    { open = False
    , entries = []
    , generation = 0
    , hashingId = Nothing
    , importingId = Nothing
    , started = False
    , held = False
    , tagsDraft = ""
    , artistDraft = ""
    , sourcePrefix = "folder-import"
    , collectionId = Nothing
    , duplicatePolicy = SkipDuplicates
    , capturedDuplicatePolicy = SkipDuplicates
    , capturedCollectionId = Nothing
    , overflow = 0
    , status = Nothing
    , lookedUp = Set.empty
    , durableEnabled = False
    , saveStatus = MemoryOnly
    , checkpointSeq = 0
    , pendingBegin = Nothing
    , savedSummary = Nothing
    , savedCheckpointId = Nothing
    , replaceNeedsConfirm = False
    , awaitingResume = False
    }


setOpen : Bool -> Model -> Model
setOpen open model =
    { model | open = open }


pathSegments : String -> List String
pathSegments path =
    String.split "/" path
        |> List.map (String.replace "\\" "/")
        |> List.concatMap (String.split "/")
        |> List.filter ((/=) "")


isHiddenPath : String -> Bool
isHiddenPath path =
    List.any (\segment -> String.startsWith "." segment && String.length segment > 1) (pathSegments path)


isPackagePath : String -> Bool
isPackagePath path =
    List.any
        (\segment ->
            let
                lower =
                    String.toLower segment
            in
            String.endsWith ".photoslibrary" lower || String.endsWith ".photolibrary" lower
        )
        (pathSegments path)


isMetadataFile : String -> Bool
isMetadataFile name =
    let
        lower =
            String.toLower name
    in
    List.member lower [ ".ds_store", "thumbs.db", "desktop.ini" ]
        || String.startsWith "._" lower


classify : FileFact -> Result String ()
classify fact =
    let
        path =
            if String.trim fact.relativePath == "" then
                fact.name

            else
                fact.relativePath

        base =
            pathSegments path |> List.reverse |> List.head |> Maybe.withDefault fact.name
    in
    if isPackagePath path then
        Err "Inside a photo library package; export the image first."

    else if isHiddenPath path then
        Err "Hidden path component."

    else if isMetadataFile base then
        Err "Folder metadata file."

    else
        Queue.validateFile { name = fact.name, mime = fact.mime, size = fact.size }


isValidHash : String -> Bool
isValidHash value =
    String.startsWith "sha256:" value
        && String.length value == 71
        && (String.dropLeft 7 value
                |> String.toList
                |> List.all (\c -> Char.isDigit c || (c >= 'a' && c <= 'f'))
           )


isCanonicalPostId : String -> Bool
isCanonicalPostId value =
    case String.toInt value of
        Just number ->
            number > 0 && String.fromInt number == value

        Nothing ->
            False


isPickBlocked : Model -> Bool
isPickBlocked model =
    model.hashingId
        /= Nothing
        || model.importingId
        /= Nothing
        || List.any
            (\entry ->
                case entry.phase of
                    PendingHash ->
                        True

                    Hashing ->
                        True

                    QueuedImport ->
                        True

                    Importing _ ->
                        True

                    _ ->
                        False
            )
            model.entries


phaseLabel : Phase -> String
phaseLabel phase =
    case phase of
        Excluded _ ->
            "Excluded"

        PendingHash ->
            "Waiting to hash"

        Hashing ->
            "Hashing"

        Hashed ->
            "Ready"

        HashFailed _ ->
            "Hash failed"

        QueuedImport ->
            "Queued"

        Importing _ ->
            "Importing"

        Imported _ ->
            "Imported"

        Skipped _ ->
            "Skipped duplicate"

        Failed _ ->
            "Failed"

        Uncertain _ ->
            "Uncertain"


type alias Counts =
    { total : Int
    , supported : Int
    , excluded : Int
    , unsupported : Int
    , oversize : Int
    , ready : Int
    , imported : Int
    , skipped : Int
    , failed : Int
    , uncertain : Int
    }


counts : Model -> Counts
counts model =
    let
        isSupported e =
            case e.phase of
                Excluded _ ->
                    False

                _ ->
                    True

        countWhere pred =
            List.filter pred model.entries |> List.length
    in
    { total = List.length model.entries
    , supported = countWhere isSupported
    , excluded = countWhere (\e -> not (isSupported e))
    , unsupported =
        countWhere
            (\e ->
                case e.phase of
                    Excluded message ->
                        String.contains "not supported" message || String.contains "not a supported" message || String.contains "Hidden" message || String.contains "metadata" message || String.contains "package" message

                    _ ->
                        False
            )
    , oversize =
        countWhere
            (\e ->
                case e.phase of
                    Excluded message ->
                        String.contains "25 MiB" message

                    _ ->
                        False
            )
    , ready = countWhere (\e -> e.phase == Hashed)
    , imported =
        countWhere
            (\e ->
                case e.phase of
                    Imported _ ->
                        True

                    _ ->
                        False
            )
    , skipped =
        countWhere
            (\e ->
                case e.phase of
                    Skipped _ ->
                        True

                    _ ->
                        False
            )
    , failed =
        countWhere
            (\e ->
                case e.phase of
                    Failed _ ->
                        True

                    HashFailed _ ->
                        True

                    _ ->
                        False
            )
    , uncertain =
        countWhere
            (\e ->
                case e.phase of
                    Uncertain _ ->
                        True

                    _ ->
                        False
            )
    }


canStart : Model -> Bool
canStart model =
    not model.started
        && model.hashingId
        == Nothing
        && model.importingId
        == Nothing
        && List.any (\e -> e.phase == Hashed) model.entries


update : Access -> Event -> Model -> ( Model, List Effect, List Note )
update access event model =
    case event of
        PanelOpened ->
            ( { model | open = True }, [], [] )

        PanelClosed ->
            ( { model | open = False }, [], [] )

        TagsEdited value ->
            ( { model | tagsDraft = value }, [], [] )

        ArtistEdited value ->
            ( { model | artistDraft = value }, [], [] )

        SourcePrefixEdited value ->
            ( { model | sourcePrefix = value }, [], [] )

        CollectionSelected value ->
            ( { model | collectionId = value }, [], [] )

        PolicySelected policy ->
            ( { model | duplicatePolicy = policy }, [], [] )

        PickFailed message ->
            ( { model | status = Just message }, [], [] )

        FilesChosen generation facts ->
            filesChosen access generation facts model

        HashSucceeded result ->
            hashSucceeded access result model

        HashErrored result ->
            if result.generation /= model.generation then
                ( model, [], [] )

            else
                case findEntry result.entryId model of
                    Just entry ->
                        if entry.phase == Hashing then
                            pumpHash access
                                { model
                                    | hashingId = Nothing
                                    , entries = replaceEntry { entry | phase = HashFailed result.message } model.entries
                                }

                        else
                            ( model, [], [] )

                    Nothing ->
                        ( model, [], [] )

        DuplicatesArrived result ->
            if result.generation /= model.generation then
                ( model, [], [] )

            else
                ( { model
                    | entries =
                        List.map
                            (\entry ->
                                case entry.hash of
                                    Just hash ->
                                        case List.filter (\m -> m.hash == hash) result.matches |> List.head of
                                            Just match ->
                                                { entry | duplicatePostId = Just match.postId }

                                            Nothing ->
                                                entry

                                    Nothing ->
                                        entry
                            )
                            model.entries
                  }
                , []
                , []
                )

        DuplicatesFailed result ->
            if result.generation /= model.generation then
                ( model, [], [] )

            else
                ( { model | status = Just ("Duplicate check failed: " ++ result.message ++ " Import can still use the server check.") }, [], [] )

        StartRequested ->
            start access model

        ImportSettled settled ->
            importSettled access settled model

        DismissRequested entryId ->
            dismiss access entryId model

        FinishedCleared ->
            let
                kept =
                    List.filter (not << isTerminal) model.entries

                dropped =
                    List.map .id model.entries |> List.filter (\id -> not (List.member id (List.map .id kept)))

                cleared =
                    { model | entries = kept }

                ( checkpointed, checkpointEffects ) =
                    requestCheckpoint cleared
            in
            ( checkpointed
            , (if List.isEmpty dropped then
                []

               else
                [ ReleaseFiles dropped ]
              )
                ++ checkpointEffects
            , []
            )

        DurableEnabledChanged enabled ->
            durableEnabledChanged access enabled model

        ReplaceSavedConfirmed ->
            if not model.replaceNeedsConfirm then
                ( model, [], [] )

            else
                let
                    confirmed =
                        { model | replaceNeedsConfirm = False, savedSummary = Nothing, durableEnabled = True }

                    ( checkpointed, effects ) =
                        requestCheckpoint confirmed
                in
                ( checkpointed, effects, [] )

        CheckpointStored checkpointId ->
            checkpointStored access checkpointId model

        CheckpointFailed checkpointId message ->
            checkpointFailed checkpointId message model

        SavedProbeArrived summary ->
            case summary of
                Just found ->
                    if model.savedCheckpointId == Just found.checkpointId && model.durableEnabled then
                        ( model, [], [] )

                    else
                        ( { model | savedSummary = Just found }, [], [] )

                Nothing ->
                    ( { model | savedSummary = Nothing }, [], [] )

        SavedProbeFailed message ->
            ( { model | status = Just ("Saved-import check failed: " ++ message ++ " This import stays memory-only until a save succeeds.") }, [], [] )

        RestoreSavedRequested ->
            case model.savedSummary of
                Just _ ->
                    ( model, [ RestoreSavedCheckpoint ], [] )

                Nothing ->
                    ( { model | status = Just "There is no saved import to resume." }, [], [] )

        SavedRestored currentApiBase checkpoint ->
            restoreSaved access currentApiBase checkpoint model

        SavedRestoreFailed message ->
            ( { model | status = Just ("Could not resume the saved import: " ++ message) }, [], [] )

        DiscardSavedRequested ->
            if model.savedSummary == Nothing && model.savedCheckpointId == Nothing then
                ( { model | status = Just "There is no saved import to discard." }, [], [] )

            else
                ( model, [ DeleteSavedCheckpoint ], [] )

        SavedDiscarded ->
            ( { model
                | savedSummary = Nothing
                , savedCheckpointId = Nothing
                , durableEnabled = False
                , saveStatus = MemoryOnly
                , replaceNeedsConfirm = False
                , status = Just "Saved import discarded. Only the browser copies were removed; nothing in the library changed."
              }
            , []
            , []
            )

        SavedDiscardFailed message ->
            ( { model | status = Just ("Could not discard the saved import: " ++ message) }, [], [] )

        ExplicitResumeRequested ->
            explicitResume access model


isTerminal : Entry -> Bool
isTerminal entry =
    case entry.phase of
        Imported _ ->
            True

        Skipped _ ->
            True

        Failed _ ->
            True

        Uncertain _ ->
            True

        Excluded _ ->
            True

        HashFailed _ ->
            True

        _ ->
            False


findEntry : Int -> Model -> Maybe Entry
findEntry entryId model =
    List.filter (\e -> e.id == entryId) model.entries |> List.head


replaceEntry : Entry -> List Entry -> List Entry
replaceEntry entry entries =
    List.map (\e -> if e.id == entry.id then entry else e) entries


sourceFor : String -> String -> String
sourceFor prefix relativePath =
    let
        trimmed =
            String.trim prefix
    in
    if trimmed == "" then
        relativePath

    else
        trimmed ++ "/" ++ relativePath


filesChosen : Access -> Int -> List FileFact -> Model -> ( Model, List Effect, List Note )
filesChosen access generation facts model =
    if generation <= model.generation then
        ( model, [], [] )

    else if isPickBlocked model then
        ( { model | status = Just "Wait for the current preview or import to settle before picking another folder." }, [], [] )

    else
        let
            tags =
                Queue.parseTags model.tagsDraft

            artist =
                String.trim model.artistDraft

            classified =
                List.indexedMap (\index fact -> ( index, fact, classify fact )) facts

            supportedFacts =
                List.filterMap
                    (\( index, fact, result ) ->
                        case result of
                            Ok () ->
                                Just ( index, fact )

                            Err _ ->
                                Nothing
                    )
                    classified

            keptSupported =
                List.take maxSupported supportedFacts

            overflow =
                max 0 (List.length supportedFacts - maxSupported)

            keptSet =
                Set.fromList (List.map Tuple.first keptSupported)

            entries =
                classified
                    |> List.filterMap
                        (\( index, fact, result ) ->
                            case result of
                                Err message ->
                                    Just ( index, fact, Err message )

                                Ok () ->
                                    if Set.member index keptSet then
                                        Just ( index, fact, Ok () )

                                    else
                                        Nothing
                        )
                    |> List.map
                        (\( index, fact, result ) ->
                            { id = index
                            , name = fact.name
                            , relativePath =
                                if String.trim fact.relativePath == "" then
                                    fact.name

                                else
                                    fact.relativePath
                            , size = fact.size
                            , mime = fact.mime
                            , lastModified = fact.lastModified
                            , hash = Nothing
                            , duplicatePostId = Nothing
                            , tags = tags
                            , source = sourceFor model.sourcePrefix (if String.trim fact.relativePath == "" then fact.name else fact.relativePath)
                            , artist = artist
                            , phase =
                                case result of
                                    Ok () ->
                                        PendingHash

                                    Err message ->
                                        Excluded message
                            }
                        )

            next =
                { model
                    | entries = entries
                    , generation = generation
                    , hashingId = Nothing
                    , importingId = Nothing
                    , started = False
                    , held = False
                    , capturedDuplicatePolicy = SkipDuplicates
                    , capturedCollectionId = Nothing
                    , overflow = overflow
                    , lookedUp = Set.empty
                    , status =
                        Just
                            (if overflow > 0 then
                                "Folder has more than 1000 supported images. Only the first 1000 are in this preview; split the folder and import the rest separately."

                             else
                                "Preview ready. Hashing locally; no uploads have started."
                            )
                }

            droppedIds =
                classified
                    |> List.filterMap
                        (\( index, fact, result ) ->
                            case result of
                                Err _ ->
                                    Just index

                                Ok () ->
                                    if Set.member index keptSet then
                                        Nothing

                                    else
                                        Just index
                        )

            ( pumped, effects, notes ) =
                pumpHash access next

            ( checkpointed, checkpointEffects ) =
                requestCheckpoint pumped
        in
        -- Replaced-preview ids are reused by the new pick, so only this
        -- pick's excluded/overflow ids are released (to both stores).
        ( checkpointed
        , (if List.isEmpty droppedIds then
            effects

           else
            ReleaseFiles droppedIds :: effects
          )
            ++ checkpointEffects
        , notes
        )


pumpHash : Access -> Model -> ( Model, List Effect, List Note )
pumpHash _ model =
    if model.hashingId /= Nothing then
        ( model, [], [] )

    else
        case List.filter (\e -> e.phase == PendingHash) model.entries |> List.head of
            Just next ->
                ( { model
                    | hashingId = Just next.id
                    , entries = replaceEntry { next | phase = Hashing } model.entries
                  }
                , [ HashEntry model.generation next.id ]
                , []
                )

            Nothing ->
                maybeLookup model


maybeLookup : Model -> ( Model, List Effect, List Note )
maybeLookup model =
    let
        hashes =
            List.filterMap
                (\e ->
                    case ( e.phase, e.hash ) of
                        ( Hashed, Just hash ) ->
                            if Set.member hash model.lookedUp then
                                Nothing

                            else
                                Just hash

                        _ ->
                            Nothing
                )
                model.entries
                |> unique
    in
    if List.isEmpty hashes then
        ( model, [], [] )

    else
        ( { model | lookedUp = Set.union (Set.fromList hashes) model.lookedUp }
        , List.map (LookupHashes model.generation) (ImportApi.batchHashes hashes)
        , []
        )


unique : List String -> List String
unique items =
    List.foldl
        (\item acc ->
            if List.member item acc then
                acc

            else
                acc ++ [ item ]
        )
        []
        items


hashSucceeded : Access -> { entryId : Int, generation : Int, hash : String, size : Int, lastModified : Int } -> Model -> ( Model, List Effect, List Note )
hashSucceeded access result model =
    if result.generation /= model.generation then
        ( model, [], [] )

    else
        case findEntry result.entryId model of
            Just entry ->
                if entry.phase /= Hashing then
                    ( model, [], [] )

                else if not (isValidHash result.hash) then
                    pumpHashThenCheckpoint access
                        { model
                            | hashingId = Nothing
                            , entries = replaceEntry { entry | phase = HashFailed "The browser returned an invalid hash for this file." } model.entries
                        }

                else if result.size /= entry.size || result.lastModified /= entry.lastModified then
                    pumpHashThenCheckpoint access
                        { model
                            | hashingId = Nothing
                            , entries = replaceEntry { entry | phase = HashFailed "This file changed after it was picked. Pick it again before importing." } model.entries
                        }

                else
                    pumpHashThenCheckpoint access
                        { model
                            | hashingId = Nothing
                            , entries = replaceEntry { entry | phase = Hashed, hash = Just result.hash } model.entries
                        }

            Nothing ->
                ( model, [], [] )


start : Access -> Model -> ( Model, List Effect, List Note )
start access model =
    if model.awaitingResume then
        explicitResume access model

    else if not access.writesEnabled then
        ( { model | status = Just "Unlock to start the import." }, [], [] )

    else if model.hashingId /= Nothing || List.any (\e -> e.phase == PendingHash) model.entries then
        ( { model | status = Just "Wait for local hashing to finish before starting." }, [], [] )

    else if model.started then
        ( model, [], [] )

    else if not (List.any (\e -> e.phase == Hashed) model.entries) then
        ( { model | status = Just "There are no ready files to import." }, [], [] )

    else
        let
            entries =
                List.map
                    (\entry ->
                        if entry.phase /= Hashed then
                            entry

                        else
                            { entry
                                | phase = QueuedImport
                                , tags = Queue.parseTags model.tagsDraft
                                , artist = String.trim model.artistDraft
                                , source = sourceFor model.sourcePrefix entry.relativePath
                            }
                    )
                    model.entries

            next =
                { model
                    | entries = entries
                    , started = True
                    , held = False
                    , capturedDuplicatePolicy = model.duplicatePolicy
                    , capturedCollectionId = model.collectionId
                    , status = Just "Import started."
                }

            ( pumped, effects, notes ) =
                pumpImport access next
        in
        ( pumped, effects, notes )


pumpImport : Access -> Model -> ( Model, List Effect, List Note )
pumpImport access model =
    if model.held || model.awaitingResume || not access.writesEnabled || model.importingId /= Nothing then
        ( model, [], [] )

    else
        case List.filter (\e -> e.phase == QueuedImport) model.entries |> List.head of
            Just next ->
                if model.durableEnabled then
                    -- Persist intent BEFORE the request: the entry is
                    -- recorded as importing in the checkpoint, and the
                    -- upload only begins when that save commits.
                    if model.pendingBegin /= Nothing then
                        ( model, [], [] )

                    else
                        let
                            checkpointId =
                                model.checkpointSeq + 1
                        in
                        ( { model
                            | importingId = Just next.id
                            , pendingBegin = Just next.id
                            , checkpointSeq = checkpointId
                            , saveStatus = SavePending checkpointId
                            , entries = replaceEntry { next | phase = Importing access.accessGeneration } model.entries
                          }
                        , [ SaveCheckpoint checkpointId ]
                        , []
                        )

                else
                    ( { model
                        | importingId = Just next.id
                        , entries = replaceEntry { next | phase = Importing access.accessGeneration } model.entries
                      }
                    , [ BeginImport next.id ]
                    , []
                    )

            Nothing ->
                ( model, [], [] )


resume : Access -> Model -> ( Model, List Effect, List Note )
resume access model =
    if not access.writesEnabled then
        ( model, [], [] )

    else if model.awaitingResume then
        -- A restored import never restarts from an unlock alone; it
        -- waits for the explicit Resume/Start action.
        ( model, [], [] )

    else
        pumpImport access { model | held = False }


withReceiptCheckpoint : ( Model, List Effect, List Note ) -> ( Model, List Effect, List Note )
withReceiptCheckpoint ( model, effects, notes ) =
    let
        ( checkpointed, checkpointEffects ) =
            requestCheckpoint model
    in
    ( checkpointed, effects ++ checkpointEffects, notes )


pumpHashThenCheckpoint : Access -> Model -> ( Model, List Effect, List Note )
pumpHashThenCheckpoint access model =
    let
        ( pumped, effects, notes ) =
            pumpHash access model

        stillHashing =
            List.any (\e -> e.phase == PendingHash || e.phase == Hashing) pumped.entries
    in
    if stillHashing then
        ( pumped, effects, notes )

    else
        withReceiptCheckpoint ( pumped, effects, notes )


explicitResume : Access -> Model -> ( Model, List Effect, List Note )
explicitResume access model =
    if not access.writesEnabled then
        ( { model | status = Just "Unlock to resume the import." }, [], [] )

    else
        pumpImport access { model | held = False, awaitingResume = False }


{-| Ask storage for a fresh checkpoint of the current state. No-op in
memory-only mode, with no entries, or while a save is in flight (the
in-flight snapshot is superseded by the next trigger).
-}
requestCheckpoint : Model -> ( Model, List Effect )
requestCheckpoint model =
    if not model.durableEnabled || List.isEmpty model.entries then
        ( model, [] )

    else
        case model.saveStatus of
            SavePending _ ->
                ( model, [] )

            _ ->
                let
                    checkpointId =
                        model.checkpointSeq + 1
                in
                ( { model | checkpointSeq = checkpointId, saveStatus = SavePending checkpointId }
                , [ SaveCheckpoint checkpointId ]
                )


durableEnabledChanged : Access -> Bool -> Model -> ( Model, List Effect, List Note )
durableEnabledChanged access enabled model =
    if enabled then
        case model.savedSummary of
            Just summary ->
                if model.savedCheckpointId /= Just summary.checkpointId && not (List.isEmpty model.entries) then
                    ( { model
                        | replaceNeedsConfirm = True
                        , status = Just "A different saved import already exists. Confirm replacing it before the current import is kept for reload; the saved import is not discarded until you confirm."
                      }
                    , []
                    , []
                    )

                else
                    enableDurable model

            Nothing ->
                enableDurable model

    else
        -- Explicit switch to memory-only: a gated upload may proceed
        -- now that no durable intent checkpoint is required.
        let
            switched =
                { model
                    | durableEnabled = False
                    , saveStatus = MemoryOnly
                    , replaceNeedsConfirm = False
                    , pendingBegin = Nothing
                    , status = Just "This import is memory-only now. Reloading will lose it."
                }
        in
        case model.pendingBegin of
            Just entryId ->
                case findEntry entryId model of
                    Just entry ->
                        case entry.phase of
                            Importing _ ->
                                ( switched, [ BeginImport entryId ], [] )

                            _ ->
                                pumpImport access switched

                    Nothing ->
                        pumpImport access switched

            Nothing ->
                -- A failed intent checkpoint already reverted its
                -- entry to queued; switching to memory-only releases
                -- the queue through the normal pump.
                pumpImport access switched


enableDurable : Model -> ( Model, List Effect, List Note )
enableDurable model =
    let
        enabled =
            { model | durableEnabled = True, replaceNeedsConfirm = False }

        ( checkpointed, effects ) =
            requestCheckpoint enabled
    in
    ( if List.isEmpty model.entries then
        { checkpointed | status = Just "This import will be kept for reload once files are picked." }

      else
        checkpointed
    , effects
    , []
    )


checkpointStored : Access -> Int -> Model -> ( Model, List Effect, List Note )
checkpointStored access checkpointId model =
    case model.saveStatus of
        SavePending pendingId ->
            if pendingId /= checkpointId then
                -- Late ack for a superseded save: it must not relabel
                -- or release the newer queue state.
                ( model, [], [] )

            else
                let
                    stored =
                        { model
                            | saveStatus = SaveStored checkpointId
                            , savedCheckpointId = Just checkpointId
                            , savedSummary = Nothing
                        }
                in
                case model.pendingBegin of
                    Just entryId ->
                        if not access.writesEnabled then
                            case findEntry entryId model of
                                Just entry ->
                                    ( { stored
                                        | pendingBegin = Nothing
                                        , importingId = Nothing
                                        , entries = replaceEntry { entry | phase = QueuedImport } stored.entries
                                        , status = Just "Unlock to start the import."
                                      }
                                    , []
                                    , []
                                    )

                                Nothing ->
                                    ( { stored | pendingBegin = Nothing, importingId = Nothing }, [], [] )

                        else
                            ( { stored | pendingBegin = Nothing }, [ BeginImport entryId ], [] )

                    Nothing ->
                        ( stored, [], [] )

        _ ->
            ( model, [], [] )


checkpointFailed : Int -> String -> Model -> ( Model, List Effect, List Note )
checkpointFailed checkpointId message model =
    case model.saveStatus of
        SavePending pendingId ->
            if pendingId /= checkpointId then
                ( model, [], [] )

            else
                case model.pendingBegin of
                    Just entryId ->
                        -- No mutation may be sent when the chosen
                        -- durable intent checkpoint failed.
                        case findEntry entryId model of
                            Just entry ->
                                ( { model
                                    | pendingBegin = Nothing
                                    , importingId = Nothing
                                    , saveStatus = SaveFailed message
                                    , entries = replaceEntry { entry | phase = QueuedImport } model.entries
                                    , status = Just ("Could not save the import checkpoint, so nothing was uploaded: " ++ message ++ " Keep trying to save, or switch to memory-only to import without reload safety.")
                                  }
                                , []
                                , []
                                )

                            Nothing ->
                                ( { model | pendingBegin = Nothing, importingId = Nothing, saveStatus = SaveFailed message }, [], [] )

                    Nothing ->
                        ( { model
                            | saveStatus = SaveFailed message
                            , status = Just ("Could not save the import checkpoint: " ++ message ++ " This import is still usable in memory; reload safety is off until a save succeeds.")
                          }
                        , []
                        , []
                        )

        _ ->
            ( model, [], [] )


phaseToCheckpoint : Phase -> CheckpointPhase
phaseToCheckpoint phase =
    case phase of
        Excluded message ->
            CpExcluded message

        PendingHash ->
            CpPendingHash

        Hashing ->
            CpHashing

        Hashed ->
            CpHashed

        HashFailed message ->
            CpHashFailed message

        QueuedImport ->
            CpQueuedImport

        Importing _ ->
            CpImporting

        Imported info ->
            CpImported info

        Skipped postId ->
            CpSkipped postId

        Failed message ->
            CpFailed message

        Uncertain message ->
            CpUncertain message


needsBlob : CheckpointPhase -> Bool
needsBlob phase =
    case phase of
        CpExcluded _ ->
            False

        CpHashFailed _ ->
            False

        CpImported _ ->
            False

        CpSkipped _ ->
            False

        CpFailed _ ->
            False

        CpUncertain _ ->
            False

        _ ->
            True


{-| Build the plain-data checkpoint for the current model. Only file
facts, hashes, captured metadata, policy/collection targets, and
receipts are included — never a credential, token, access state, or
object URL, because none of those exist in this state.
-}
buildCheckpoint : String -> Model -> Checkpoint
buildCheckpoint apiBase model =
    { schema = checkpointSchema
    , checkpointId = model.checkpointSeq
    , apiBase = canonicalApiBase apiBase
    , generation = model.generation
    , started = model.started
    , tagsDraft = model.tagsDraft
    , artistDraft = model.artistDraft
    , sourcePrefix = model.sourcePrefix
    , collectionId = model.collectionId
    , duplicatePolicy = policyString model.duplicatePolicy
    , capturedDuplicatePolicy = policyString model.capturedDuplicatePolicy
    , capturedCollectionId = model.capturedCollectionId
    , overflow = model.overflow
    , entries =
        List.map
            (\entry ->
                { id = entry.id
                , name = entry.name
                , relativePath = entry.relativePath
                , size = entry.size
                , mime = entry.mime
                , lastModified = entry.lastModified
                , hash = entry.hash
                , duplicatePostId = entry.duplicatePostId
                , tags = entry.tags
                , source = entry.source
                , artist = entry.artist
                , phase = phaseToCheckpoint entry.phase
                , hasBlob = needsBlob (phaseToCheckpoint entry.phase)
                , hashVerified = True
                }
            )
            model.entries
    }


checkpointTotalBytes : Checkpoint -> Int
checkpointTotalBytes checkpoint =
    List.map .size checkpoint.entries |> List.sum


restoreSaved : Access -> String -> Checkpoint -> Model -> ( Model, List Effect, List Note )
restoreSaved access currentApiBase checkpoint model =
    case restoreCheckpoint currentApiBase checkpoint model of
        Ok ( restored, effects ) ->
            let
                ( pumped, pumpEffects, notes ) =
                    pumpHash access restored
            in
            ( pumped, effects ++ pumpEffects, notes )

        Err message ->
            ( { model | status = Just ("Could not resume the saved import: " ++ message) }, [], [] )


{-| Validate and restore a checkpoint against the live canonical API
base. Terminal receipts stay terminal; importing/uncertain work becomes
explicit `Uncertain` and is never auto-resubmitted; hashing/queued work
resumes only from verified bytes. Nothing starts uploading here.
-}
restoreCheckpoint : String -> Checkpoint -> Model -> Result String ( Model, List Effect )
restoreCheckpoint currentApiBase checkpoint model =
    if checkpoint.schema /= checkpointSchema then
        Err "The saved import uses an unsupported format and cannot be resumed."

    else if canonicalApiBase checkpoint.apiBase /= canonicalApiBase currentApiBase then
        Err "The saved import belongs to a different API and was not sent there. Discard it or open the original API to resume it."

    else if List.length checkpoint.entries > maxSupported then
        Err "The saved import has more than 1000 files and cannot be resumed."

    else if checkpointTotalBytes checkpoint > maxTotalBytes then
        Err "The saved import is larger than 250 MiB in total and cannot be resumed."

    else if hasDuplicateIds checkpoint.entries then
        Err "The saved import is damaged (duplicate file entries) and cannot be resumed."

    else if not (List.isEmpty model.entries) && model.savedCheckpointId /= Just checkpoint.checkpointId then
        Err "Finish or clear the current preview before resuming a saved import."

    else
        case validateCheckpointEntries checkpoint.entries of
            Just problem ->
                Err problem

            Nothing ->
                let
                    entries =
                        List.map restoreEntry checkpoint.entries

                    anyQueued =
                        List.any (\e -> e.phase == QueuedImport) entries

                    restored =
                        { model
                            | entries = entries
                            , generation = checkpoint.generation
                            , hashingId = Nothing
                            , importingId = Nothing
                            , started = checkpoint.started
                            , held = checkpoint.started && anyQueued
                            , awaitingResume = checkpoint.started && anyQueued
                            , tagsDraft = checkpoint.tagsDraft
                            , artistDraft = checkpoint.artistDraft
                            , sourcePrefix = checkpoint.sourcePrefix
                            , collectionId = checkpoint.collectionId
                            , duplicatePolicy = policyFromString checkpoint.duplicatePolicy
                            , capturedDuplicatePolicy = policyFromString checkpoint.capturedDuplicatePolicy
                            , capturedCollectionId = checkpoint.capturedCollectionId
                            , overflow = checkpoint.overflow
                            , lookedUp =
                                Set.fromList
                                    (List.filterMap
                                        (\e ->
                                            if isTerminal e then
                                                e.hash

                                            else
                                                Nothing
                                        )
                                        entries
                                    )
                            , durableEnabled = True
                            , saveStatus = SaveStored checkpoint.checkpointId
                            , checkpointSeq = checkpoint.checkpointId
                            , pendingBegin = Nothing
                            , savedSummary = Nothing
                            , savedCheckpointId = Just checkpoint.checkpointId
                            , replaceNeedsConfirm = False
                            , status = Just "Saved import restored. Nothing uploads until you resume or start it; files that were importing need review."
                        }
                in
                Ok ( restored, [] )


hasDuplicateIds : List CheckpointEntry -> Bool
hasDuplicateIds entries =
    let
        ids =
            List.map .id entries
    in
    List.length ids /= (Set.fromList ids |> Set.size)


validateCheckpointEntries : List CheckpointEntry -> Maybe String
validateCheckpointEntries entries =
    List.filterMap validateCheckpointEntry entries |> List.head


validateCheckpointEntry : CheckpointEntry -> Maybe String
validateCheckpointEntry entry =
    if entry.size < 0 then
        Just "The saved import is damaged (a file has a negative size) and cannot be resumed."

    else if needsBlob entry.phase && entry.size <= 0 then
        Just ("The saved copy of " ++ entry.relativePath ++ " is empty and cannot be resumed.")

    else
        case entry.phase of
            CpImported info ->
                if not (isCanonicalPostId info.postId) then
                    Just "The saved import is damaged (a completed receipt has no valid post) and cannot be resumed."

                else
                    validateCheckpointHash entry

            CpSkipped postId ->
                if not (isCanonicalPostId postId) then
                    Just "The saved import is damaged (a skipped receipt has no valid post) and cannot be resumed."

                else
                    validateCheckpointHash entry

            CpHashed ->
                validateCheckpointHash entry

            CpQueuedImport ->
                validateCheckpointHash entry

            CpImporting ->
                validateCheckpointHash entry

            _ ->
                Nothing


validateCheckpointHash : CheckpointEntry -> Maybe String
validateCheckpointHash entry =
    case entry.hash of
        Just hash ->
            if isValidHash hash then
                Nothing

            else
                Just ("The saved import is damaged (" ++ entry.relativePath ++ " has an invalid hash) and cannot be resumed.")

        Nothing ->
            Just ("The saved import is damaged (" ++ entry.relativePath ++ " has no hash) and cannot be resumed.")


restoreEntry : CheckpointEntry -> Entry
restoreEntry entry =
    let
        base =
            { id = entry.id
            , name = entry.name
            , relativePath = entry.relativePath
            , size = entry.size
            , mime = entry.mime
            , lastModified = entry.lastModified
            , hash = entry.hash
            , duplicatePostId = entry.duplicatePostId
            , tags = entry.tags
            , source = entry.source
            , artist = entry.artist
            , phase = PendingHash
            }
    in
    case entry.phase of
        CpExcluded message ->
            { base | phase = Excluded message }

        CpPendingHash ->
            if entry.hasBlob then
                { base | phase = PendingHash }

            else
                { base | phase = HashFailed "The saved file copy is missing. Pick the file again to import it." }

        CpHashing ->
            if entry.hasBlob then
                { base | phase = PendingHash }

            else
                { base | phase = HashFailed "The saved file copy is missing. Pick the file again to import it." }

        CpHashed ->
            if not entry.hasBlob then
                { base | phase = HashFailed "The saved file copy is missing. Pick the file again to import it." }

            else if entry.hashVerified then
                { base | phase = Hashed }

            else
                { base | phase = PendingHash }

        CpHashFailed message ->
            { base | phase = HashFailed message }

        CpQueuedImport ->
            if entry.hasBlob && entry.hashVerified then
                { base | phase = QueuedImport }

            else
                { base | phase = Uncertain "The saved copy of this queued file could not be verified, so it was not resubmitted. Needs review." }

        CpImporting ->
            { base | phase = Uncertain "This file was importing when the saved import was closed. It may already exist; it was not resubmitted. Needs review." }

        CpImported info ->
            { base | phase = Imported info }

        CpSkipped postId ->
            { base | phase = Skipped postId }

        CpFailed message ->
            { base | phase = Failed message }

        CpUncertain message ->
            { base | phase = Uncertain message }


importSettled : Access -> { entryId : Int, generation : Int, outcome : ImportOutcome } -> Model -> ( Model, List Effect, List Note )
importSettled access settled model =
    case findEntry settled.entryId model of
        Nothing ->
            ( model, [], [] )

        Just entry ->
            case entry.phase of
                Importing generation ->
                    if generation /= settled.generation then
                        ( model, [], [] )

                    else
                        withReceiptCheckpoint (settle access settled.outcome entry { model | importingId = Nothing })

                _ ->
                    ( model, [], [] )


receiptProblem : Entry -> Maybe String -> DuplicatePolicy -> ImportSuccess -> Maybe String
receiptProblem entry captured capturedPolicy success =
    let
        nonblankError =
            case success.attachmentError of
                Just message ->
                    String.trim message /= ""

                Nothing ->
                    False
    in
    if not (isCanonicalPostId success.post.id) then
        Just "The import receipt did not name a valid post. The post may already exist; it was not resubmitted."

    else if entry.hash /= Just success.post.hash then
        Just "The import receipt was for different content than this file. The post may already exist; it was not resubmitted."

    else if success.collectionId /= captured then
        Just "The import receipt named a different collection than the import target. The post may already exist; it was not resubmitted."

    else if captured == Nothing && success.attached then
        Just "The import receipt claimed a collection attachment without a target collection. The post may already exist; it was not resubmitted."

    else if captured /= Nothing && not success.attached && not nonblankError then
        Just "The import receipt did not confirm attachment to the target collection. The post may already exist; it was not resubmitted."

    else if captured /= Nothing && success.attached && nonblankError then
        Just "The import receipt claimed both attachment and an attachment failure. The post may already exist; it was not resubmitted."

    else if capturedPolicy == ErrorOnDuplicates && success.duplicate then
        Just "The import receipt reported a duplicate while the duplicate policy is error. The post may already exist; it was not resubmitted."

    else
        Nothing


settle : Access -> ImportOutcome -> Entry -> Model -> ( Model, List Effect, List Note )
settle access outcome entry model =
    case outcome of
        ImportSucceeded success ->
            case receiptProblem entry model.capturedCollectionId model.capturedDuplicatePolicy success of
                Just problem ->
                    let
                        next =
                            { model | entries = replaceEntry { entry | phase = Uncertain problem } model.entries }

                        ( pumped, effects, notes ) =
                            pumpImport access next
                    in
                    ( pumped, ReleaseFile entry.id :: effects, notes )

                Nothing ->
                    let
                        phase =
                            if success.duplicate && model.capturedDuplicatePolicy == SkipDuplicates then
                                Skipped success.post.id

                            else
                                Imported
                                    { postId = success.post.id
                                    , duplicate = success.duplicate
                                    , attached = success.attached
                                    , attachmentError = success.attachmentError
                                    }

                        next =
                            { model | entries = replaceEntry { entry | phase = phase } model.entries }

                        ( pumped, effects, notes ) =
                            pumpImport access next
                    in
                    ( pumped, ReleaseFile entry.id :: effects, ImportConfirmed success :: notes )

        ImportFailed failed ->
            if failed.authRejected && entryGeneration entry == access.accessGeneration then
                ( { model
                    | entries = replaceEntry { entry | phase = Failed failed.message } model.entries
                    , held = True
                    , status = Just "Import held: authorization was rejected. Unlock with a credential that can write, then resume."
                  }
                , [ ReleaseFile entry.id ]
                , [ CredentialRejected (entryGeneration entry) failed.authStatus ]
                )

            else if failed.authRejected then
                let
                    next =
                        { model
                            | entries = replaceEntry { entry | phase = Failed failed.message } model.entries
                            , held = False
                        }

                    ( pumped, effects, notes ) =
                        pumpImport access next
                in
                ( pumped
                , ReleaseFile entry.id :: effects
                , CredentialRejected (entryGeneration entry) failed.authStatus :: notes
                )

            else
                let
                    next =
                        { model | entries = replaceEntry { entry | phase = Failed failed.message } model.entries }

                    ( pumped, effects, notes ) =
                        pumpImport access next
                in
                ( pumped, ReleaseFile entry.id :: effects, notes )

        ImportUncertain message ->
            let
                next =
                    { model | entries = replaceEntry { entry | phase = Uncertain message } model.entries }

                ( pumped, effects, notes ) =
                    pumpImport access next
            in
            ( pumped, ReleaseFile entry.id :: effects, notes )


entryGeneration : Entry -> Int
entryGeneration entry =
    case entry.phase of
        Importing generation ->
            generation

        _ ->
            -1


dismiss : Access -> Int -> Model -> ( Model, List Effect, List Note )
dismiss access entryId model =
    case findEntry entryId model of
        Nothing ->
            ( model, [], [] )

        Just entry ->
            case entry.phase of
                Hashing ->
                    ( model, [], [] )

                Importing _ ->
                    ( model, [], [] )

                _ ->
                    let
                        next =
                            { model | entries = List.filter (\e -> e.id /= entryId) model.entries }

                        ( pumped, effects, notes ) =
                            pumpHash access next
                    in
                    withReceiptCheckpoint ( pumped, ReleaseFile entryId :: effects, notes )
