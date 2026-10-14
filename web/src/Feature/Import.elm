port module Feature.Import exposing
    ( Context
    , Entry
    , Model
    , Msg(..)
    , OutMsg(..)
    , context
    , counts
    , init
    , isOpen
    , resume
    , setOpen
    , state
    , subscriptions
    , update
    , view
    )

{-| Browser folder-import coordinator.

Picked `File` values live only in memory, keyed by the pure state's
entry ids. Hashing runs through the native bridge one file at a time;
uploads run through `Api.Import` one file at a time, using the
credential supplied for that call. No file, hash list, or credential
is written to storage, a URL, or a log by this module.
-}

import Api.Import as ImportApi
import Dict exposing (Dict)
import Domain.Collection exposing (CollectionSummary)
import Domain.Post exposing (PostDetail)
import Feature.Import.State as State
import File exposing (File)
import Html exposing (Html, button, div, h2, input, label, li, option, p, select, span, text, ul)
import Html.Attributes as Attr exposing (attribute, class, disabled, id, placeholder, selected, title, type_, value)
import Html.Events exposing (onClick, onInput)
import Http
import Json.Decode as Decode
import Json.Encode as Encode


port importPickRequested : Encode.Value -> Cmd msg


port importFilesPicked : (Decode.Value -> msg) -> Sub msg


port importPickCancelled : (Int -> msg) -> Sub msg


port importHashRequested : Encode.Value -> Cmd msg


port importHashCompleted : (Decode.Value -> msg) -> Sub msg


port importFilesReleased : Encode.Value -> Cmd msg


type alias Context =
    { apiBase : String
    , credential : Maybe String
    , writesEnabled : Bool
    , accessGeneration : Int
    }


context : String -> Maybe String -> Bool -> Int -> Context
context apiBase credential writesEnabled accessGeneration =
    { apiBase = apiBase
    , credential = credential
    , writesEnabled = writesEnabled
    , accessGeneration = accessGeneration
    }


type alias Entry =
    State.Entry


type alias Model =
    { state : State.Model
    , files : Dict Int File
    , pickerPending : Bool
    , pickerGeneration : Int
    }


type Msg
    = OpenPanel
    | ClosePanel
    | TagsChanged String
    | ArtistChanged String
    | SourcePrefixChanged String
    | CollectionChanged String
    | PolicyChanged String
    | PickFolderClicked
    | PickFilesClicked
    | PickCancelled Int
    | Picked Decode.Value
    | HashCompleted Decode.Value
    | DuplicatesCompleted Int (Result Http.Error (List ImportApi.DuplicateMatch))
    | StartClicked
    | ImportFinished Int Int (Result Http.Error ImportApi.ImportResult)
    | DismissEntry Int
    | ClearFinished
    | ViewPost String


type OutMsg
    = ImportConfirmed ImportApi.ImportResult
    | CredentialRejected Int Int
    | OpenPost String


init : Model
init =
    { state = State.init, files = Dict.empty, pickerPending = False, pickerGeneration = 0 }


isOpen : Model -> Bool
isOpen model =
    model.state.open


setOpen : Bool -> Model -> Model
setOpen open model =
    { model | state = State.setOpen open model.state }


state : Model -> State.Model
state model =
    model.state


counts : Model -> State.Counts
counts model =
    State.counts model.state


toAccess : Context -> State.Access
toAccess ctx =
    { writesEnabled = ctx.writesEnabled, accessGeneration = ctx.accessGeneration }


update : Context -> Msg -> Model -> ( Model, Cmd Msg, List OutMsg )
update ctx msg model =
    case msg of
        OpenPanel ->
            run ctx model State.PanelOpened

        ClosePanel ->
            run ctx model State.PanelClosed

        TagsChanged value ->
            run ctx model (State.TagsEdited value)

        ArtistChanged value ->
            run ctx model (State.ArtistEdited value)

        SourcePrefixChanged value ->
            run ctx model (State.SourcePrefixEdited value)

        CollectionChanged value ->
            run ctx model
                (State.CollectionSelected
                    (if String.trim value == "" then
                        Nothing

                     else
                        Just value
                    )
                )

        PolicyChanged value ->
            run ctx model (State.PolicySelected (State.policyFromString value))

        PickFolderClicked ->
            requestPick ctx "folder" model

        PickFilesClicked ->
            requestPick ctx "files" model

        PickCancelled generation ->
            if generation == model.pickerGeneration && model.pickerPending then
                ( { model | pickerPending = False }, Cmd.none, [] )

            else
                ( model, Cmd.none, [] )

        Picked value ->
            case Decode.decodeValue pickedDecoder value of
                Ok picked ->
                    if picked.generation /= model.pickerGeneration || not model.pickerPending then
                        ( model, Cmd.none, [] )

                    else if State.isPickBlocked model.state then
                        run ctx { model | pickerPending = False } (State.PickFailed "Wait for the current preview or import to settle before picking another folder.")

                    else
                        let
                            files =
                                Dict.fromList (List.indexedMap (\index item -> ( index, item.file )) picked.files)

                            facts =
                                List.map .fact picked.files
                        in
                        runWithFiles ctx { model | pickerPending = False, files = files } (State.FilesChosen picked.generation facts)

                Err _ ->
                    run ctx { model | pickerPending = False } (State.PickFailed "The browser returned files this app could not read. Try picking the folder again.")

        HashCompleted value ->
            case Decode.decodeValue hashResultDecoder value of
                Ok (Ok result) ->
                    run ctx model (State.HashSucceeded result)

                Ok (Err result) ->
                    run ctx model (State.HashErrored result)

                Err _ ->
                    ( model, Cmd.none, [] )

        DuplicatesCompleted generation result ->
            case result of
                Ok matches ->
                    run ctx model (State.DuplicatesArrived { generation = generation, matches = matches })

                Err error ->
                    run ctx model (State.DuplicatesFailed { generation = generation, message = httpMessage error })

        StartClicked ->
            run ctx model State.StartRequested

        ImportFinished entryId generation result ->
            run ctx model
                (State.ImportSettled
                    { entryId = entryId
                    , generation = generation
                    , outcome =
                        case result of
                            Ok importResult ->
                                State.ImportSucceeded (toSuccess importResult)

                            Err (Http.BadStatus 409) ->
                                State.ImportFailed
                                    { message = "A matching image already exists and the duplicate policy is error."
                                    , authRejected = False
                                    , authStatus = 0
                                    }

                            Err (Http.BadStatus status) ->
                                if status == 401 || status == 403 then
                                    State.ImportFailed
                                        { message = "Authorization was rejected (HTTP " ++ String.fromInt status ++ ")."
                                        , authRejected = True
                                        , authStatus = status
                                        }

                                else if status >= 500 && status <= 599 then
                                    State.ImportUncertain ("The server returned an error (HTTP " ++ String.fromInt status ++ "). The post may already exist; it was not resubmitted.")

                                else
                                    State.ImportFailed
                                        { message = "Import failed (HTTP " ++ String.fromInt status ++ "). The post was not confirmed saved."
                                        , authRejected = False
                                        , authStatus = 0
                                        }

                            Err Http.Timeout ->
                                State.ImportUncertain "The server stopped responding. The post may already exist; it was not resubmitted."

                            Err Http.NetworkError ->
                                State.ImportUncertain "The connection was lost. The post may already exist; it was not resubmitted."

                            Err (Http.BadBody _) ->
                                State.ImportUncertain "The server answered with an unreadable response. The post may already exist; it was not resubmitted."

                            Err (Http.BadUrl url) ->
                                State.ImportFailed { message = "Invalid API URL: " ++ url, authRejected = False, authStatus = 0 }
                    }
                )

        DismissEntry entryId ->
            run ctx model (State.DismissRequested entryId)

        ClearFinished ->
            run ctx model State.FinishedCleared

        ViewPost postId ->
            ( model, Cmd.none, [ OpenPost postId ] )


requestPick : Context -> String -> Model -> ( Model, Cmd Msg, List OutMsg )
requestPick ctx mode model =
    if not ctx.writesEnabled then
        run ctx model (State.PickFailed "Unlock to pick a folder.")

    else if model.pickerPending || State.isPickBlocked model.state then
        run ctx model (State.PickFailed "Wait for the current preview or import to settle before picking another folder.")

    else
        let
            generation =
                model.pickerGeneration + 1
        in
        ( { model | pickerPending = True, pickerGeneration = generation }
        , importPickRequested
            (Encode.object
                [ ( "mode", Encode.string mode )
                , ( "generation", Encode.int generation )
                ]
            )
        , []
        )


toSuccess : ImportApi.ImportResult -> State.ImportSuccess
toSuccess result =
    { post = result.post
    , duplicate = result.duplicate
    , collectionId = result.collectionId
    , attached = result.attached
    , attachmentError = result.attachmentError
    }


resume : Context -> Model -> ( Model, Cmd Msg, List OutMsg )
resume ctx model =
    let
        ( nextState, effects, notes ) =
            State.resume (toAccess ctx) model.state
    in
    runEffects ctx { model | state = nextState } effects notes


run : Context -> Model -> State.Event -> ( Model, Cmd Msg, List OutMsg )
run ctx model event =
    let
        ( nextState, effects, notes ) =
            State.update (toAccess ctx) event model.state
    in
    runEffects ctx { model | state = nextState } effects notes


runWithFiles : Context -> Model -> State.Event -> ( Model, Cmd Msg, List OutMsg )
runWithFiles ctx model event =
    run ctx model event


runEffects : Context -> Model -> List State.Effect -> List State.Note -> ( Model, Cmd Msg, List OutMsg )
runEffects ctx model effects notes =
    let
        dropped =
            List.concatMap
                (\effect ->
                    case effect of
                        State.ReleaseFile entryId ->
                            [ entryId ]

                        State.ReleaseFiles ids ->
                            ids

                        _ ->
                            []
                )
                effects

        next =
            { model | files = List.foldl Dict.remove model.files dropped }
    in
    ( next
    , Cmd.batch (List.map (execute ctx next) effects)
    , List.map toOutMsg notes
    )


toOutMsg : State.Note -> OutMsg
toOutMsg note =
    case note of
        State.ImportConfirmed success ->
            ImportConfirmed
                { post = success.post
                , duplicate = success.duplicate
                , collectionId = success.collectionId
                , attached = success.attached
                , attachmentError = success.attachmentError
                }

        State.CredentialRejected generation status ->
            CredentialRejected generation status


execute : Context -> Model -> State.Effect -> Cmd Msg
execute ctx model effect =
    case effect of
        State.HashEntry generation entryId ->
            case Dict.get entryId model.files of
                Just _ ->
                    importHashRequested
                        (Encode.object
                            [ ( "entryId", Encode.int entryId )
                            , ( "generation", Encode.int generation )
                            ]
                        )

                Nothing ->
                    Cmd.none

        State.LookupHashes generation hashes ->
            ImportApi.lookupDuplicates ctx.apiBase hashes (DuplicatesCompleted generation)

        State.BeginImport entryId ->
            case ( Dict.get entryId model.files, findEntry entryId model.state ) of
                ( Just file, Just entry ) ->
                    case entry.hash of
                        Just hash ->
                            ImportApi.requestImport ctx.apiBase
                                ctx.credential
                                file
                                { tags = entry.tags
                                , source = entry.source
                                , artist = entry.artist
                                , expectedHash = hash
                                , duplicatePolicy = State.policyString model.state.capturedDuplicatePolicy
                                , collectionId = model.state.capturedCollectionId
                                }
                                (ImportFinished entryId ctx.accessGeneration)

                        Nothing ->
                            Cmd.none

                _ ->
                    Cmd.none

        State.ReleaseFile entryId ->
            importFilesReleased
                (Encode.object
                    [ ( "generation", Encode.int model.state.generation )
                    , ( "ids", Encode.list Encode.int [ entryId ] )
                    ]
                )

        State.ReleaseFiles ids ->
            importFilesReleased
                (Encode.object
                    [ ( "generation", Encode.int model.state.generation )
                    , ( "ids", Encode.list Encode.int ids )
                    ]
                )


findEntry : Int -> State.Model -> Maybe State.Entry
findEntry entryId importState =
    List.filter (\e -> e.id == entryId) importState.entries |> List.head


httpMessage : Http.Error -> String
httpMessage error =
    case error of
        Http.BadStatus status ->
            "HTTP " ++ String.fromInt status

        Http.Timeout ->
            "request timed out"

        Http.NetworkError ->
            "network error"

        Http.BadBody _ ->
            "invalid response"

        Http.BadUrl url ->
            "invalid URL: " ++ url


type alias PickedItem =
    { file : File, fact : State.FileFact }


type alias PickedEnvelope =
    { generation : Int, files : List PickedItem }


pickedDecoder : Decode.Decoder PickedEnvelope
pickedDecoder =
    Decode.map2 PickedEnvelope
        (Decode.field "generation" Decode.int)
        (Decode.field "files"
            (Decode.list
                (Decode.map2 PickedItem
                    (Decode.field "file" File.decoder)
                    (Decode.map5 State.FileFact
                        (Decode.field "name" Decode.string)
                        (Decode.oneOf [ Decode.field "path" Decode.string, Decode.field "name" Decode.string ])
                        (Decode.field "size" Decode.int)
                        (Decode.oneOf [ Decode.field "type" Decode.string, Decode.succeed "" ])
                        (Decode.oneOf [ Decode.field "lastModified" Decode.int, Decode.succeed 0 ])
                    )
                )
            )
        )


hashResultDecoder : Decode.Decoder (Result { entryId : Int, generation : Int, message : String } { entryId : Int, generation : Int, hash : String, size : Int, lastModified : Int })
hashResultDecoder =
    Decode.map2 Tuple.pair
        (Decode.field "entryId" Decode.int)
        (Decode.field "generation" Decode.int)
        |> Decode.andThen
            (\( entryId, generation ) ->
                Decode.oneOf
                    [ Decode.field "error" Decode.string
                        |> Decode.map (\message -> Err { entryId = entryId, generation = generation, message = message })
                    , Decode.map3 (\hash size lastModified -> Ok { entryId = entryId, generation = generation, hash = hash, size = size, lastModified = lastModified })
                        (Decode.field "hash" Decode.string)
                        (Decode.field "size" Decode.int)
                        (Decode.field "lastModified" Decode.int)
                    ]
            )


subscriptions : Model -> Sub Msg
subscriptions _ =
    Sub.batch
        [ importFilesPicked Picked
        , importPickCancelled PickCancelled
        , importHashCompleted HashCompleted
        ]


view : Bool -> List CollectionSummary -> Model -> Html Msg
view writesEnabled collections model =
    if not model.state.open then
        text ""

    else
        let
            importState =
                model.state

            summary =
                State.counts importState
        in
        div [ class "upload-panel", attribute "role" "dialog", attribute "aria-label" "Import a folder" ]
            [ div [ class "panel-header" ]
                [ h2 [ class "panel-title" ] [ text "Import folder" ]
                , button [ class "button button-quiet", type_ "button", onClick ClosePanel, attribute "aria-label" "Close import panel" ] [ text "×" ]
                ]
            , div [ class "panel-body" ]
                [ div [ class "panel-section" ]
                    [ div [ class "upload-field" ]
                        [ label [ class "upload-label", attribute "for" "import-tags" ] [ text "Tags (shared)" ]
                        , input [ class "upload-input", id "import-tags", type_ "text", placeholder "cat, demo", value importState.tagsDraft, onInput TagsChanged ] []
                        ]
                    , div [ class "upload-field" ]
                        [ label [ class "upload-label", attribute "for" "import-artist" ] [ text "Artist (shared, optional)" ]
                        , input [ class "upload-input", id "import-artist", type_ "text", value importState.artistDraft, onInput ArtistChanged ] []
                        ]
                    , div [ class "upload-field" ]
                        [ label [ class "upload-label", attribute "for" "import-source-prefix" ] [ text "Source prefix" ]
                        , input [ class "upload-input", id "import-source-prefix", type_ "text", value importState.sourcePrefix, onInput SourcePrefixChanged ] []
                        ]
                    , div [ class "upload-field" ]
                        [ label [ class "upload-label", attribute "for" "import-collection" ] [ text "Destination collection (optional)" ]
                        , select [ class "upload-input", id "import-collection", onInput CollectionChanged ]
                            (option [ value "", selected (importState.collectionId == Nothing) ] [ text "No collection" ]
                                :: List.map
                                    (\collection ->
                                        option [ value collection.id, selected (importState.collectionId == Just collection.id) ] [ text collection.name ]
                                    )
                                    collections
                            )
                        ]
                    , div [ class "upload-field" ]
                        [ label [ class "upload-label", attribute "for" "import-policy" ] [ text "Duplicates" ]
                        , select [ class "upload-input", id "import-policy", onInput PolicyChanged ]
                            [ option [ value "skip", selected (importState.duplicatePolicy == State.SkipDuplicates) ] [ text "Skip duplicates" ]
                            , option [ value "error", selected (importState.duplicatePolicy == State.ErrorOnDuplicates) ] [ text "Fail on duplicates" ]
                            ]
                        ]
                    , p [ class "panel-note" ] [ text "JPEG, PNG, or GIF up to 25 MiB each; at most 1000 supported images per folder. Picking only previews and hashes locally — nothing uploads until you start the import." ]
                    , div [ class "upload-item-actions" ]
                        [ button [ class "button", type_ "button", onClick PickFolderClicked, disabled (not writesEnabled || model.pickerPending || State.isPickBlocked importState) ] [ text "Choose folder…" ]
                        , button [ class "button button-quiet", type_ "button", onClick PickFilesClicked, disabled (not writesEnabled || model.pickerPending || State.isPickBlocked importState) ] [ text "Choose files…" ]
                        ]
                    , if writesEnabled then
                        text ""

                      else
                        p [ class "panel-note" ] [ text "Unlock to import images." ]
                    ]
                , div [ class "panel-section" ]
                    [ h2 [ class "panel-section-title" ] [ text "Preview" ]
                    , previewSummary importState summary
                    , entryList importState
                    , div [ class "upload-item-actions" ]
                        [ button
                            [ class "button"
                            , type_ "button"
                            , onClick StartClicked
                            , disabled (not writesEnabled || not (State.canStart importState))
                            ]
                            [ text "Start import" ]
                        , if List.any State.isTerminal importState.entries then
                            button [ class "button button-quiet", type_ "button", onClick ClearFinished ] [ text "Clear finished" ]

                          else
                            text ""
                        ]
                    , p [ class "panel-note" ] [ text "Picked files stay in this browser tab's memory only. Reloading loses them and they cannot be resumed here; the CLI import remains the durable, resumable path." ]
                    ]
                ]
            ]


previewSummary : State.Model -> State.Counts -> Html Msg
previewSummary importState summary =
    div []
        ([ p [ class "panel-note" ]
            [ text
                (String.fromInt summary.supported
                    ++ " supported, "
                    ++ String.fromInt summary.excluded
                    ++ " excluded ("
                    ++ String.fromInt summary.unsupported
                    ++ " unsupported, "
                    ++ String.fromInt summary.oversize
                    ++ " oversize), "
                    ++ String.fromInt summary.ready
                    ++ " ready. Imported "
                    ++ String.fromInt summary.imported
                    ++ ", skipped "
                    ++ String.fromInt summary.skipped
                    ++ ", failed "
                    ++ String.fromInt summary.failed
                    ++ ", uncertain "
                    ++ String.fromInt summary.uncertain
                    ++ "."
                )
            ]
         , case importState.capturedCollectionId of
            Just collectionId ->
                p [ class "panel-note" ] [ text ("Import target collection: " ++ collectionId) ]

            Nothing ->
                if importState.started then
                    p [ class "panel-note" ] [ text "Import target: no collection." ]

                else
                    text ""
         ]
            ++ (case importState.status of
                    Just message ->
                        [ p [ class "panel-note", attribute "role" "status" ] [ text message ] ]

                    Nothing ->
                        []
               )
        )


entryList : State.Model -> Html Msg
entryList importState =
    if List.isEmpty importState.entries then
        p [ class "panel-note" ] [ text "No folder picked yet." ]

    else
        ul [ class "upload-queue" ] (List.map entryView importState.entries)


entryView : State.Entry -> Html Msg
entryView entry =
    li [ class "upload-item" ]
        [ div [ class "upload-item-head" ]
            [ span [ class "upload-name", title entry.relativePath ] [ text entry.relativePath ]
            , span [ class "upload-size" ] [ text (String.fromInt entry.size ++ " B") ]
            , span [ class "upload-phase" ] [ text (State.phaseLabel entry.phase) ]
            ]
        , case entry.duplicatePostId of
            Just postId ->
                p [ class "upload-note" ] [ text ("Already in the library as post #" ++ postId ++ ".") ]

            Nothing ->
                text ""
        , phaseDetail entry
        , div [ class "upload-item-actions" ]
            [ case entry.phase of
                State.Imported info ->
                    button [ class "button button-quiet", type_ "button", onClick (ViewPost info.postId) ] [ text "View" ]

                State.Skipped postId ->
                    button [ class "button button-quiet", type_ "button", onClick (ViewPost postId) ] [ text "View" ]

                _ ->
                    text ""
            , if isDismissible entry then
                button [ class "button button-quiet", type_ "button", onClick (DismissEntry entry.id), attribute "aria-label" ("Dismiss " ++ entry.relativePath) ] [ text "Dismiss" ]

              else
                text ""
            ]
        ]


isDismissible : State.Entry -> Bool
isDismissible entry =
    case entry.phase of
        State.Hashing ->
            False

        State.Importing _ ->
            False

        _ ->
            True


phaseDetail : State.Entry -> Html Msg
phaseDetail entry =
    case entry.phase of
        State.Excluded message ->
            p [ class "upload-error", attribute "role" "alert" ] [ text message ]

        State.HashFailed message ->
            p [ class "upload-error", attribute "role" "alert" ] [ text message ]

        State.Failed message ->
            p [ class "upload-error", attribute "role" "alert" ] [ text message ]

        State.Uncertain message ->
            p [ class "upload-error", attribute "role" "alert" ] [ text message ]

        State.Imported info ->
            div []
                [ p [ class "upload-note" ] [ text ("Saved as post #" ++ info.postId ++ ".") ]
                , case info.attachmentError of
                    Just message ->
                        p [ class "upload-error", attribute "role" "alert" ] [ text ("The post was saved, but attaching it to the collection failed: " ++ message ++ " It will not be uploaded again.") ]

                    Nothing ->
                        if info.attached then
                            p [ class "upload-note" ] [ text "Attached to the target collection." ]

                        else
                            text ""
                ]

        State.Skipped postId ->
            p [ class "upload-note" ] [ text ("Skipped; already post #" ++ postId ++ ".") ]

        _ ->
            text ""
