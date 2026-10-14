module Feature.Import.State exposing
    ( Access
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
    , canStart
    , isCanonicalPostId
    , isPickBlocked
    , isTerminal
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


type Effect
    = HashEntry Int Int
    | LookupHashes Int (List String)
    | BeginImport Int
    | ReleaseFile Int
    | ReleaseFiles (List Int)


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
            in
            ( { model | entries = kept }
            , if List.isEmpty dropped then
                []

              else
                [ ReleaseFiles dropped ]
            , []
            )


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
        in
        -- Replaced-preview ids are reused by the new pick, so only this
        -- pick's excluded/overflow ids are released (to both stores).
        ( pumped
        , (if List.isEmpty droppedIds then
            effects

           else
            ReleaseFiles droppedIds :: effects
          )
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
                    pumpHash access
                        { model
                            | hashingId = Nothing
                            , entries = replaceEntry { entry | phase = HashFailed "The browser returned an invalid hash for this file." } model.entries
                        }

                else if result.size /= entry.size || result.lastModified /= entry.lastModified then
                    pumpHash access
                        { model
                            | hashingId = Nothing
                            , entries = replaceEntry { entry | phase = HashFailed "This file changed after it was picked. Pick it again before importing." } model.entries
                        }

                else
                    pumpHash access
                        { model
                            | hashingId = Nothing
                            , entries = replaceEntry { entry | phase = Hashed, hash = Just result.hash } model.entries
                        }

            Nothing ->
                ( model, [], [] )


start : Access -> Model -> ( Model, List Effect, List Note )
start access model =
    if not access.writesEnabled then
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
    if model.held || not access.writesEnabled || model.importingId /= Nothing then
        ( model, [], [] )

    else
        case List.filter (\e -> e.phase == QueuedImport) model.entries |> List.head of
            Just next ->
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

    else
        pumpImport access { model | held = False }


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
                        settle access settled.outcome entry { model | importingId = Nothing }

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
                    ( pumped, ReleaseFile entryId :: effects, notes )
